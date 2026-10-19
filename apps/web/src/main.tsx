import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Tailwind CSS 4 from the design tokens, the self-hosted fonts and the screen kit's component layer
// (fy-* classes, the same the designs use): src/styles/globals.css.
import "./styles/globals.css";
import "./app.css";
import { startAppearance } from "./appearance";
import { router } from "./router";

startAppearance();

const root = document.getElementById("root");
if (!root) throw new Error("index.html has no #root element");
createRoot(root).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
);
