import { Check, Loader, Circle } from "lucide-react";
import { useT } from "../lib/i18n";

export interface Step {
  id: string;
  label: string;
  status: "completed" | "in_progress" | "pending";
  turnIndex?: number;
}

export interface ProgressStepperProps {
  steps: Step[];
  onStepClick?: (turnIndex: number) => void;
}

// ProgressStepper renders a horizontal progress bar at the top of the chat
// area showing the agent's multi-step task progress. Each step displays an
// icon (✅ completed / ⏳ in-progress / ⬚ pending) and a label. Completed
// steps are clickable to jump to the corresponding message via onStepClick.
export function ProgressStepper({ steps, onStepClick }: ProgressStepperProps) {
  const t = useT();

  if (steps.length === 0) return null;

  const completedCount = steps.filter((s) => s.status === "completed").length;
  const currentStep = steps.find((s) => s.status === "in_progress");
  const pct = Math.round((completedCount / steps.length) * 100);

  const handleClick = (step: Step) => {
    if (step.status === "completed" && step.turnIndex !== undefined && onStepClick) {
      onStepClick(step.turnIndex);
    }
  };

  return (
    <div className="progress-stepper" role="navigation" aria-label={t("progressStepper.label")}>
      <div className="progress-stepper__header">
        <span className="progress-stepper__pct">{pct}%</span>
        <span className="progress-stepper__counter">
          {t("progressStepper.stepProgress", { current: currentStep ? steps.indexOf(currentStep) + 1 : completedCount, total: steps.length })}
        </span>
        {currentStep && (
          <span className="progress-stepper__current-label">{currentStep.label}</span>
        )}
      </div>

      <div className="progress-stepper__track">
        <div
          className="progress-stepper__fill"
          style={{ width: `${pct}%` }}
        />
      </div>

      <ol className="progress-stepper__steps">
        {steps.map((step) => {
          const clickable = step.status === "completed" && step.turnIndex !== undefined && !!onStepClick;
          return (
            <li
              key={step.id}
              className={[
                "progress-stepper__step",
                `progress-stepper__step--${step.status}`,
                clickable ? "progress-stepper__step--clickable" : "",
              ].filter(Boolean).join(" ")}
              onClick={() => handleClick(step)}
              role={clickable ? "button" : undefined}
              tabIndex={clickable ? 0 : undefined}
              aria-label={t("progressStepper.jumpToStep", { label: step.label })}
              onKeyDown={(e) => {
                if (clickable && (e.key === "Enter" || e.key === " ")) {
                  e.preventDefault();
                  handleClick(step);
                }
              }}
            >
              <span className="progress-stepper__icon">
                {step.status === "completed" ? (
                  <Check size={14} className="progress-stepper__icon--completed" />
                ) : step.status === "in_progress" ? (
                  <Loader size={14} className="progress-stepper__icon--progress" />
                ) : (
                  <Circle size={14} className="progress-stepper__icon--pending" />
                )}
              </span>
              <span className="progress-stepper__label">{step.label}</span>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
