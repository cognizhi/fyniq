import { useEffect, useState } from "react";
import { t } from "./i18n";
import { NewPerson } from "./pages/NewPerson";
import { Packs } from "./pages/Packs";
import { Policy } from "./pages/Policy";
import { Features } from "./pages/Features";
import { Directory } from "./pages/Directory";
import { Home } from "./pages/Home";
import { Join } from "./pages/Join";
import { VerifyLetter } from "./pages/VerifyLetter";
import { LeaverHome } from "./pages/LeaverHome";
import { RequestLetter } from "./pages/RequestLetter";
import { Letters } from "./pages/Letters";
import { MyDetails } from "./pages/MyDetails";
import { AskQuestion } from "./pages/AskQuestion";
import { PeopleOnboarding } from "./pages/PeopleOnboarding";
import { AskAssistant } from "./pages/AskAssistant";
import { AssistantSettings } from "./pages/AssistantSettings";
import { MyPay, Payslip } from "./pages/MyPay";
import { Erasures } from "./pages/Erasures";
import { Concerns } from "./pages/Concerns";
import { Grievances } from "./pages/Grievances";
import { Goals } from "./pages/Goals";
import { Hiring } from "./pages/Hiring";
import { FyniqLink } from "./pages/FyniqLink";
import { rememberPage, switchProvider, switchedIn } from "./workspace";
import { PayRunApproval } from "./pages/PayRunApproval";
import { PayrollHR } from "./pages/PayrollHR";
import { PeopleRequests } from "./pages/PeopleRequests";
import { LetterTemplates } from "./pages/LetterTemplates";
import { MyRequests } from "./pages/MyRequests";
import { GovernmentLeave } from "./pages/GovernmentLeave";
import { ReportSick } from "./pages/ReportSick";
import { ApplyLeave } from "./pages/ApplyLeave";
import { TeamLeave } from "./pages/TeamLeave";
import { MyLeave } from "./pages/MyLeave";
import { NotFound } from "./pages/NotFound";
import { Person } from "./pages/Person";
import { SignIn, signInURL } from "./pages/SignIn";
import { navigate } from "./route";
import { matchRoute } from "./registry";
import { useSession } from "./session";

// joinKind is the kind of code the join page asks for, from its address: setup, recovery, or (anything else) an invitation.
function joinKind(): "setup" | "invitation" | "recovery" {
  const k = new URLSearchParams(window.location.search).get("kind");
  return k === "setup" || k === "recovery" ? k : "invitation";
}

// pathname follows the address bar, so the browser's back and forward buttons and `navigate` both move the app.
function usePathname(): string {
  const [path, set] = useState(window.location.pathname);
  useEffect(() => {
    const on = () => set(window.location.pathname);
    window.addEventListener("popstate", on);
    return () => window.removeEventListener("popstate", on);
  }, []);
  return path;
}

// The app's pages. A signed-out person goes to sign-in and comes back to where they were; a signed-in one never sees sign-in, and lands by role.
export function App() {
  const path = usePathname();
  const state = useSession();
  const matched = matchRoute(path);
  const route = matched?.route;
  const signedOut = state.status === "none";
  const landing = state.status === "in" ? state.session.landing : "/sign-in";
  useEffect(() => {
    if (route?.signedIn && state.status === "in") rememberPage(path); // the switch brings a person back to the page they left here
  }, [path, route, state.status]);
  useEffect(() => {
    if (state.status === "loading") return;
    // Crossing from Fyniq with the provider the person signed in with: ask that provider at once, so the sign-in page is not shown again (one sign-in across the apps). A refused one comes back to /sign-in with its code.
    const atRoot = (path === "/");
    const via = atRoot && state.status === "none" && new URLSearchParams(window.location.search).get("switch") === "1" ? switchProvider(window.location.search) : null;
    if (via) window.location.replace(signInURL(via, "/?switch=1"));
    else if (atRoot) navigate((state.status === "in" && switchedIn(window.location.search)) || landing, true);
    else if (route?.signedIn && signedOut) navigate("/sign-in", true);
    else if ((route?.id === "sign-in" || route?.id === "join") && state.status === "in") navigate(landing, true);
  }, [path, route, state.status, landing, signedOut]);

  const body = (() => {
    if (route?.id === "verify-letter") return <VerifyLetter />; // public: no sign-in, no session wait (NM-LTR-05)
    if (state.status === "loading") return <p role="status" className="fy-secondary">{t("session.loading")}</p>;
    if (path === "/join") return <Join kind={joinKind()} />;
    if (path === "/sign-in") return <SignIn returnTo={landing === "/sign-in" ? "/" : landing} />;
    if (state.status === "in" && state.session.leaver) return <LeaverHome session={state.session} />;
    if (!route) return <NotFound home={landing} />;
    if (state.status === "in" && route.id === "people-directory") return <Directory roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "person-new") return <NewPerson roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "packs") return <Packs roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "policy") return <Policy roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "features") return <Features roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "apply-leave") return <ApplyLeave roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "report-sick") return <ReportSick roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "request-letter") return <RequestLetter roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "my-details") return <MyDetails roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "ask") return <AskQuestion roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "onboarding") return <PeopleOnboarding roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "ask-assistant") return <AskAssistant roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "assistant") return <AssistantSettings roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "my-pay") return <MyPay roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "payslip") return <Payslip id={matched?.params.id ?? ""} roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "payrun-approval") return <PayRunApproval id={matched?.params.id ?? ""} roles={state.session.roles ?? []} member={state.session.member_id} />;
    if (state.status === "in" && route.id === "my-concerns") return <Concerns roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "my-goals") return <Goals roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "hiring") return <Hiring roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "grievances") return <Grievances roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "erasures") return <Erasures roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "fyniq-link") return <FyniqLink roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "payroll-hr") return <PayrollHR roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "people-requests") return <PeopleRequests roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "letters") return <Letters roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "letter-templates") return <LetterTemplates roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "my-requests") return <MyRequests roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "government-leave") return <GovernmentLeave roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "team-leave") return <TeamLeave roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "my-leave") return <MyLeave roles={state.session.roles ?? []} />;
    if (state.status === "in" && route.id === "person-leave") return <MyLeave roles={state.session.roles ?? []} personId={matched?.params.id ?? ""} />;
    if (state.status === "in" && route.id === "person") return <Person id={matched?.params.id ?? ""} roles={state.session.roles ?? []} />;
    if (state.status === "in") return <Home session={state.session} screen={route.id === "people-home" ? "people" : "me"} />;
    return null;
  })();
  return (
    <>
      <a className="fy-btn fy-btn--primary nm-skip" href="#main">{t("app.skip")}</a>
      {body}
    </>
  );
}
