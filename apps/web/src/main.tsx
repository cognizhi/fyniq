import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "../../../docs/design-system/fyniq/tokens.css";
// The screen kit's component layer (fy-* classes), the same the designs use: P0's screens follow the
// canvases with it until P1's component system (MASTER.md §7).
import "../../../docs/design/screens/kit/ui.css";
import "./app.css";
import { App } from "./App";
import { followSystemTheme } from "./theme";

followSystemTheme();

const root = document.getElementById("root");
if (!root) throw new Error("index.html has no #root element");
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
