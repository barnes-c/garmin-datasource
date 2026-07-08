package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	garminconnect "github.com/barnes-c/go-garminconnect/garminconnect"
	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/barnesc/garminconnect/pkg/models"
)

func authTestDatasource(loginFn func(ctx context.Context, tokenFile, email, password string, prompt func() (string, error)) (*garminconnect.Client, error)) *Datasource {
	return &Datasource{
		settings: &models.PluginSettings{
			Email:   "athlete@example.com",
			Secrets: &models.SecretPluginSettings{Password: "secret"},
		},
		frameCache: map[string]cachedFrame{},
		loginFn:    loginFn,
	}
}

func submitMFACode(t *testing.T, d *Datasource, code string) *captureSender {
	t.Helper()
	sender := &captureSender{}
	err := d.CallResource(context.Background(), &backend.CallResourceRequest{
		Path:   "mfa",
		Method: "POST",
		Body:   []byte(`{"code":"` + code + `"}`),
	}, sender)
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

// TestMFAFullFlow drives the state machine end to end: query → MFA pending →
// code submitted via the resource endpoint → login completes → queries work.
func TestMFAFullFlow(t *testing.T) {
	d := authTestDatasource(func(_ context.Context, _, _, _ string, prompt func() (string, error)) (*garminconnect.Client, error) {
		code, err := prompt()
		if err != nil {
			return nil, err
		}
		if code != "424242" {
			return nil, errors.New("invalid MFA code")
		}
		return garminconnect.NewClient(""), nil
	})

	// First access starts the login; Garmin "requests" MFA, so callers get
	// the pending error instead of blocking.
	if _, err := d.garminClient(context.Background()); !errors.Is(err, errMFAPending) {
		t.Fatalf("expected errMFAPending, got %v", err)
	}

	// Health check surfaces the friendly instruction.
	res, err := d.CheckHealth(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Message, "MFA code") {
		t.Fatalf("expected MFA instruction, got %q", res.Message)
	}

	// Submitting the code completes the pending login.
	sender := submitMFACode(t, d, "424242")
	if sender.resp.Status != 200 {
		t.Fatalf("expected 200 from verify, got %d: %s", sender.resp.Status, sender.resp.Body)
	}

	// The client is now installed; no new login is started.
	client, err := d.garminClient(context.Background())
	if err != nil || client == nil {
		t.Fatalf("expected logged-in client, got %v", err)
	}
	if d.loginFailures != 0 {
		t.Errorf("expected no recorded failures, got %d", d.loginFailures)
	}
}

func TestMFAWrongCodeBacksOff(t *testing.T) {
	d := authTestDatasource(func(_ context.Context, _, _, _ string, prompt func() (string, error)) (*garminconnect.Client, error) {
		code, _ := prompt()
		if code != "424242" {
			return nil, errors.New("invalid MFA code")
		}
		return garminconnect.NewClient(""), nil
	})

	if _, err := d.garminClient(context.Background()); !errors.Is(err, errMFAPending) {
		t.Fatalf("expected errMFAPending, got %v", err)
	}

	sender := submitMFACode(t, d, "000000")
	if sender.resp.Status != 401 {
		t.Fatalf("expected 401 for wrong code, got %d", sender.resp.Status)
	}

	// The failed attempt is cleared and the backoff engaged.
	d.mu.Lock()
	blocked := time.Until(d.loginBlockedUntil) > 0
	cleared := d.login == nil
	d.mu.Unlock()
	if !blocked || !cleared {
		t.Fatalf("expected cleared attempt with backoff, blocked=%v cleared=%v", blocked, cleared)
	}
	if _, err := d.garminClient(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "next attempt allowed") {
		t.Fatalf("expected backoff error, got %v", err)
	}
}

func TestConcurrentQueriesSharePendingLogin(t *testing.T) {
	starts := 0
	d := authTestDatasource(nil)
	d.loginFn = func(_ context.Context, _, _, _ string, prompt func() (string, error)) (*garminconnect.Client, error) {
		starts++
		_, err := prompt()
		return nil, err
	}

	for i := 0; i < 3; i++ {
		if _, err := d.garminClient(context.Background()); !errors.Is(err, errMFAPending) {
			t.Fatalf("call %d: expected errMFAPending, got %v", i, err)
		}
	}
	if starts != 1 {
		t.Fatalf("expected a single login attempt for concurrent callers, got %d", starts)
	}
}
