import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api, type Session } from "./api/client";

// The session: who is signed in, from GET /v1/session (the cookie is the credential, the app holds nothing). "none" is no valid session.
export type SessionState = { status: "loading" } | { status: "none" } | { status: "in"; session: Session };

const Ctx = createContext<SessionState>({ status: "loading" });

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, set] = useState<SessionState>({ status: "loading" });
  useEffect(() => {
    let live = true;
    api.GET("/v1/session").then(({ data }) => {
      if (live) set(data ? { status: "in", session: data } : { status: "none" });
    }, () => live && set({ status: "none" }));
    return () => {
      live = false;
    };
  }, []);
  return <Ctx.Provider value={state}>{children}</Ctx.Provider>;
}

export const useSession = (): SessionState => useContext(Ctx);
