import { useCallback, useRef, useState } from "react";
import { Code2, Briefcase, Compass, User, ArrowRight, Sparkles } from "lucide-react";
import logo from "../assets/logo.svg";
import { useT, type DictKey } from "../lib/i18n";
import { app, openExternal } from "../lib/bridge";

// ── Role definitions ─────────────────────────────────────────
type UserRole = "developer" | "office" | "freelancer" | "other";

interface RoleDef {
  key: UserRole;
  icon: React.ReactNode;
  defaultMode: string;
  taskKey: DictKey;
  nameKey: DictKey;
  descKey: DictKey;
}

const ROLES: RoleDef[] = [
  { key: "developer", icon: <Code2 size={22} />, defaultMode: "coding", taskKey: "onboarding.taskDeveloper", nameKey: "onboarding.roleDeveloper", descKey: "onboarding.roleDeveloperDesc" },
  { key: "office", icon: <Briefcase size={22} />, defaultMode: "office", taskKey: "onboarding.taskOffice", nameKey: "onboarding.roleOffice", descKey: "onboarding.roleOfficeDesc" },
  { key: "freelancer", icon: <Compass size={22} />, defaultMode: "assistant", taskKey: "onboarding.taskFreelancer", nameKey: "onboarding.roleFreelancer", descKey: "onboarding.roleFreelancerDesc" },
  { key: "other", icon: <User size={22} />, defaultMode: "coding", taskKey: "onboarding.taskOther", nameKey: "onboarding.roleOther", descKey: "onboarding.roleOtherDesc" },
];

const ROLE_MODE_KEY_MAP: Record<UserRole, DictKey> = {
  developer: "workspaceType.coding",
  office: "workspaceType.office",
  freelancer: "workspaceType.assistant",
  other: "workspaceType.coding",
};

// ── Step indices ─────────────────────────────────────────────
const STEP_WELCOME = 0;
const STEP_ROLE = 1;
const STEP_APIKEY = 2;
const STEP_TASK = 3;
const TOTAL_STEPS = 4;

// ── LocalStorage helpers ─────────────────────────────────────
const USER_ROLE_KEY = "Rexion.userRole";
const ONBOARDING_TASK_PENDING_KEY = "Rexion.onboarding_task_pending";

function saveUserRole(role: UserRole): void {
  try {
    window.localStorage.setItem(USER_ROLE_KEY, role);
  } catch {
    /* ignore */
  }
}

function setOnboardingTaskPending(pending: boolean): void {
  try {
    window.localStorage.setItem(ONBOARDING_TASK_PENDING_KEY, pending ? "true" : "false");
  } catch {
    /* ignore */
  }
}

export function isOnboardingTaskPending(): boolean {
  try {
    return window.localStorage.getItem(ONBOARDING_TASK_PENDING_KEY) === "true";
  } catch {
    return false;
  }
}

export function clearOnboardingTaskPending(): void {
  setOnboardingTaskPending(false);
}

// Full-window first-run gate: multi-step onboarding flow.
// Step 0: Welcome → Step 1: Role selection → Step 2: API Key → Step 3: Guided task
export function OnboardingOverlay({ onComplete }: { onComplete: () => void }) {
  const t = useT();
  const [step, setStep] = useState(STEP_WELCOME);
  const [role, setRole] = useState<UserRole | null>(null);
  const [value, setValue] = useState("");
  const [state, setState] = useState<"idle" | "validating" | "error">("idle");
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // ── API Key submit ──────────────────────────────────────────
  const submitKey = useCallback(async () => {
    const key = value.trim();
    if (!key) {
      setError(t("onboarding.error.empty"));
      setState("error");
      inputRef.current?.focus();
      return;
    }
    setState("validating");
    setError(null);
    try {
      await app.ConnectKey(key);
      // Key validated — move to next step or complete
      setStep(STEP_TASK);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (/status\s*401|status\s*403|invalid/i.test(msg)) {
        setError(t("onboarding.error.invalid"));
      } else if (/network|unreachable|timeout|dial/i.test(msg)) {
        setError(t("onboarding.error.network"));
      } else {
        setError(msg || t("onboarding.error.unknown"));
      }
      setState("error");
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [t, value]);

  const skipKey = useCallback(() => {
    setStep(STEP_TASK);
  }, []);

  // ── Role selection handler ──────────────────────────────────
  const selectRole = useCallback((r: UserRole) => {
    setRole(r);
    saveUserRole(r);
    setStep(STEP_APIKEY);
  }, []);

  // ── Guided task: complete onboarding ────────────────────────
  const startTask = useCallback(() => {
    setOnboardingTaskPending(true);
    onComplete();
  }, [onComplete]);

  // ── Step indicator ──────────────────────────────────────────
  const stepIndicator = (
    <div className="onboarding__steps" aria-label={t("onboarding.stepIndicator", { current: step + 1, total: TOTAL_STEPS })}>
      {Array.from({ length: TOTAL_STEPS }, (_, i) => (
        <div
          key={i}
          className={[
            "onboarding__step-dot",
            i === step ? "onboarding__step-dot--active" : "",
            i < step ? "onboarding__step-dot--done" : "",
          ].join(" ")}
        />
      ))}
    </div>
  );

  // ── Step 0: Welcome ─────────────────────────────────────────
  if (step === STEP_WELCOME) {
    return (
      <div className="onboarding">
        <div className="onboarding__card onboarding__card--welcome">
          <img src={logo} className="onboarding__logo onboarding__logo--large" alt="Rexion" />
          <div className="onboarding__title onboarding__title--welcome">{t("onboarding.welcome")}</div>
          <div className="onboarding__tag">{t("onboarding.welcomeDesc")}</div>
          {stepIndicator}
          <button
            className="onboarding__submit onboarding__submit--wide"
            onClick={() => setStep(STEP_ROLE)}
          >
            {t("onboarding.start")}
            <ArrowRight size={16} />
          </button>
          <button
            type="button"
            className="onboarding__skip"
            onClick={onComplete}
          >
            {t("onboarding.skip")}
          </button>
        </div>
      </div>
    );
  }

  // ── Step 1: Role selection ──────────────────────────────────
  if (step === STEP_ROLE) {
    return (
      <div className="onboarding">
        <div className="onboarding__card onboarding__card--role">
          <div className="onboarding__title">{t("onboarding.roleTitle")}</div>
          <div className="onboarding__tag">{t("onboarding.roleDesc")}</div>
          {stepIndicator}
          <div className="onboarding__roles">
            {ROLES.map((r) => (
              <button
                key={r.key}
                className={`onboarding__role-card${role === r.key ? " onboarding__role-card--selected" : ""}`}
                onClick={() => selectRole(r.key)}
                type="button"
              >
                <span className="onboarding__role-icon">{r.icon}</span>
                <span className="onboarding__role-name">{t(r.nameKey)}</span>
                <span className="onboarding__role-desc">{t(r.descKey)}</span>
              </button>
            ))}
          </div>
          <button
            type="button"
            className="onboarding__skip"
            onClick={onComplete}
          >
            {t("onboarding.skip")}
          </button>
        </div>
      </div>
    );
  }

  // ── Step 2: API Key configuration ───────────────────────────
  if (step === STEP_APIKEY) {
    return (
      <div className="onboarding">
        <div className="onboarding__card">
          <img src={logo} className="onboarding__logo" alt="Rexion" />
          <div className="onboarding__title">{t("onboarding.title")}</div>
          <div className="onboarding__tag">{t("onboarding.tagline")}</div>
          {stepIndicator}

          <label className="onboarding__label" htmlFor="onboarding-key">
            {t("onboarding.inputLabel")}
          </label>
          <input
            id="onboarding-key"
            ref={inputRef}
            className="onboarding__input"
            type="password"
            autoComplete="off"
            spellCheck={false}
            placeholder={t("onboarding.inputPlaceholder")}
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              if (state === "error") setState("idle");
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && state !== "validating") {
                e.preventDefault();
                void submitKey();
              }
            }}
            disabled={state === "validating"}
          />

          {state === "error" && error && (
            <div className="onboarding__error" role="alert">
              {error}
            </div>
          )}

          <button
            className="onboarding__submit"
            onClick={() => void submitKey()}
            disabled={state === "validating"}
          >
            {state === "validating" ? (
              <>
                <span className="onboarding__spinner" />
                {t("onboarding.validating")}
              </>
            ) : (
              t("onboarding.submit")
            )}
          </button>

          <div className="onboarding__links">
            <button
              type="button"
              className="onboarding__link"
              onClick={() => openExternal("https://platform.deepseek.com/api_keys")}
            >
              {t("onboarding.getKey")}
            </button>
            <span className="onboarding__sep">·</span>
            <span className="onboarding__privacy">{t("onboarding.privacy")}</span>
          </div>

          <button
            type="button"
            className="onboarding__skip"
            onClick={skipKey}
            disabled={state === "validating"}
          >
            {t("onboarding.skipApiKey")}
          </button>
        </div>
      </div>
    );
  }

  // ── Step 3: Guided first task ───────────────────────────────
  const activeRole = role ?? "other";
  const activeRoleDef = ROLES.find((r) => r.key === activeRole) ?? ROLES[3];

  return (
    <div className="onboarding">
      <div className="onboarding__card onboarding__card--task">
        <div className="onboarding__task-icon">
          <Sparkles size={24} />
        </div>
        <div className="onboarding__title">{t("onboarding.guidedTaskTitle")}</div>
        <div className="onboarding__tag">{t("onboarding.guidedTaskDesc")}</div>
        {stepIndicator}
        <button
          className="onboarding__task-card"
          onClick={startTask}
          type="button"
        >
          <span className="onboarding__task-card-icon">{activeRoleDef.icon}</span>
          <div className="onboarding__task-card-body">
            <span className="onboarding__task-card-name">{t(activeRoleDef.taskKey)}</span>
            <span className="onboarding__task-card-mode">
              {t("onboarding.modePrefix")} {t(ROLE_MODE_KEY_MAP[activeRole])}
            </span>
          </div>
          <ArrowRight size={18} className="onboarding__task-card-arrow" />
        </button>
        <button
          type="button"
          className="onboarding__skip"
          onClick={onComplete}
        >
          {t("onboarding.skipTask")}
        </button>
      </div>
    </div>
  );
}
