// SaveRecipeModal is a form for saving a conversation session as a reusable Recipe.
// It captures the skill name, parameter template, and trigger configuration.
import { useState } from "react";
import { X, Save, Clock, Mail, Play } from "lucide-react";
import { useT } from "../lib/i18n";
import type { RecipeView } from "../lib/types";
import { app } from "../lib/bridge";

interface SaveRecipeModalProps {
  isOpen: boolean;
  onClose: () => void;
  initialSkill?: string;
  initialParams?: string;
  tabId?: string;
}

export function SaveRecipeModal({ isOpen, onClose, initialSkill = "", initialParams = "", tabId: _tabId }: SaveRecipeModalProps) {
  const t = useT();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [skill, setSkill] = useState(initialSkill);
  const [params, setParams] = useState(initialParams);
  const [trigger, setTrigger] = useState<"manual" | "cron" | "event">("manual");
  const [cronExpr, setCronExpr] = useState("");
  const [eventType, setEventType] = useState("mail_received");
  const [matchSender, setMatchSender] = useState("");
  const [matchSubject, setMatchSubject] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    if (!name.trim()) {
      setError(t("recipe.nameRequired"));
      return;
    }
    if (!skill.trim()) {
      setError(t("recipe.skillRequired"));
      return;
    }

    setSaving(true);
    setError(null);

    const recipe: RecipeView = {
      name: name.trim(),
      description: description.trim(),
      skill: skill.trim(),
      params: params.trim(),
      trigger,
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    if (trigger === "cron") {
      recipe.cronExpr = cronExpr.trim();
    } else if (trigger === "event") {
      recipe.eventType = eventType;
      const rules: Record<string, string> = {};
      if (matchSender.trim()) rules.sender = matchSender.trim();
      if (matchSubject.trim()) rules.subject = matchSubject.trim();
      if (Object.keys(rules).length > 0) recipe.matchRules = rules;
    }

    try {
      await app.SaveRecipe(recipe);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("recipe.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (!isOpen) return null;

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content recipe-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>{t("recipe.saveRecipe")}</h2>
          <button className="modal-close" onClick={onClose} aria-label={t("common.close")}>
            <X size={18} />
          </button>
        </div>

        <div className="modal-body">
          <div className="form-field">
            <label htmlFor="recipe-name">{t("recipe.name")}</label>
            <input
              id="recipe-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("recipe.namePlaceholder")}
              autoFocus
            />
          </div>

          <div className="form-field">
            <label htmlFor="recipe-description">{t("recipe.description")}</label>
            <input
              id="recipe-description"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("recipe.descriptionPlaceholder")}
            />
          </div>

          <div className="form-field">
            <label htmlFor="recipe-skill">{t("recipe.skill")}</label>
            <input
              id="recipe-skill"
              type="text"
              value={skill}
              onChange={(e) => setSkill(e.target.value)}
              placeholder={t("recipe.skillPlaceholder")}
            />
            <span className="form-hint">{t("recipe.skillHint")}</span>
          </div>

          <div className="form-field">
            <label htmlFor="recipe-params">{t("recipe.params")}</label>
            <textarea
              id="recipe-params"
              value={params}
              onChange={(e) => setParams(e.target.value)}
              placeholder={t("recipe.paramsPlaceholder")}
              rows={3}
            />
            <span className="form-hint">{t("recipe.paramsHint")}</span>
          </div>

          <div className="form-field">
            <label>{t("recipe.trigger")}</label>
            <div className="trigger-options">
              <button
                className={`trigger-btn ${trigger === "manual" ? "trigger-btn--active" : ""}`}
                onClick={() => setTrigger("manual")}
              >
                <Play size={16} />
                <span>{t("recipe.triggerManual")}</span>
              </button>
              <button
                className={`trigger-btn ${trigger === "cron" ? "trigger-btn--active" : ""}`}
                onClick={() => setTrigger("cron")}
              >
                <Clock size={16} />
                <span>{t("recipe.triggerCron")}</span>
              </button>
              <button
                className={`trigger-btn ${trigger === "event" ? "trigger-btn--active" : ""}`}
                onClick={() => setTrigger("event")}
              >
                <Mail size={16} />
                <span>{t("recipe.triggerEvent")}</span>
              </button>
            </div>
          </div>

          {trigger === "cron" && (
            <div className="form-field">
              <label htmlFor="recipe-cron">{t("recipe.cronExpr")}</label>
              <input
                id="recipe-cron"
                type="text"
                value={cronExpr}
                onChange={(e) => setCronExpr(e.target.value)}
                placeholder="0 9 * * 1"
              />
              <span className="form-hint">{t("recipe.cronHint")}</span>
            </div>
          )}

          {trigger === "event" && (
            <>
              <div className="form-field">
                <label htmlFor="recipe-event-type">{t("recipe.eventType")}</label>
                <select
                  id="recipe-event-type"
                  value={eventType}
                  onChange={(e) => setEventType(e.target.value)}
                >
                  <option value="mail_received">{t("recipe.eventMailReceived")}</option>
                </select>
              </div>
              <div className="form-field">
                <label htmlFor="recipe-match-sender">{t("recipe.matchSender")}</label>
                <input
                  id="recipe-match-sender"
                  type="text"
                  value={matchSender}
                  onChange={(e) => setMatchSender(e.target.value)}
                  placeholder={t("recipe.matchSenderPlaceholder")}
                />
              </div>
              <div className="form-field">
                <label htmlFor="recipe-match-subject">{t("recipe.matchSubject")}</label>
                <input
                  id="recipe-match-subject"
                  type="text"
                  value={matchSubject}
                  onChange={(e) => setMatchSubject(e.target.value)}
                  placeholder={t("recipe.matchSubjectPlaceholder")}
                />
              </div>
            </>
          )}

          {error && <div className="form-error">{error}</div>}
        </div>

        <div className="modal-footer">
          <button className="btn btn--secondary" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button className="btn btn--primary" onClick={handleSave} disabled={saving}>
            <Save size={16} />
            {saving ? t("recipe.saving") : t("recipe.save")}
          </button>
        </div>
      </div>
    </div>
  );
}