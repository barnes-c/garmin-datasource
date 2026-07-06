// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';

// jsdom has no canvas; Grafana's Combobox measures option text via a 2D
// context, so provide the minimal surface it needs.
HTMLCanvasElement.prototype.getContext = () => ({
  measureText: (text) => ({ width: text.length * 8 }),
});
