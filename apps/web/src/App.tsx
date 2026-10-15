import { motion } from "./theme";
import { Home } from "./screens/Home";
import { Install } from "./screens/Install";
import { SignIn } from "./screens/SignIn";

// The routes of the P0 web app. Each is a page: the API decides who may see what, and every screen
// reads it through the generated client (QA-API-03).
export function App() {
  const path = window.location.pathname.replace(/\/+$/, "") || "/";
  return (
    <div className="fy-device" data-motion={motion()}>
      {path === "/install" ? <Install /> : path === "/sign-in" ? <SignIn /> : <Home />}
    </div>
  );
}
