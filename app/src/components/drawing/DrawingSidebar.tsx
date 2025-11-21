import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useSelector } from "react-redux";
import { motion } from "framer-motion";
import { selectSupportModels } from "@/store/chat.ts";
import { selectMenu } from "@/store/menu.ts";
import { cn } from "@/components/ui/lib/utils.ts";
import { Button } from "@/components/ui/button.tsx";
import ModelAvatar from "@/components/ModelAvatar.tsx";
import Icon from "@/components/utils/Icon";
import Tips from "@/components/Tips";
import {
  Award,
  Bolt,
  Cpu,
  Gem,
  DollarSign,
  Eye,
  Globe,
  History,
  Image as ImageIcon,
  Github,
  Snail,
  Sparkles,
  Zap,
} from "lucide-react";
import { includingModelFromPlan } from "@/conf/subscription.tsx";
import { levelSelector } from "@/store/subscription.ts";
import { subscriptionDataSelector } from "@/store/globals.ts";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select.tsx";

const DRAWING_TAG = "image-generation";
const PLAN_INCLUDED_TAG = "plan-included";
const HIDDEN_TAGS = ["official", "fast", "unstable", "free"];

const TAG_ICON_MAP: Record<string, ReactNode> = {
  official: <Award />,
  "multi-modal": <Eye />,
  web: <Globe />,
  "high-quality": <Sparkles />,
  "high-price": <DollarSign />,
  "open-source": <Github />,
  fast: <Bolt />,
  unstable: <Snail />,
  "high-context": <Cpu />,
  free: <Zap />,
  [PLAN_INCLUDED_TAG]: <Gem />,
  [DRAWING_TAG]: <ImageIcon />,
};

const TAG_STYLE_MAP: Record<string, string> = {
  official: "text-amber-600 bg-amber-500/20",
  "multi-modal": "text-blue-600 bg-blue-500/20",
  web: "text-green-600 bg-green-500/20",
  "high-quality": "text-purple-600 bg-purple-500/20",
  "high-price": "text-red-600 bg-red-500/20",
  "open-source": "text-gray-600 bg-gray-500/20",
  "image-generation": "text-indigo-600 bg-indigo-500/20",
  fast: "text-yellow-600 bg-yellow-500/20",
  unstable: "text-orange-600 bg-orange-500/20",
  "high-context": "text-teal-600 bg-teal-500/20",
  free: "text-emerald-600 bg-emerald-500/20",
  [PLAN_INCLUDED_TAG]: "text-amber-600 bg-amber-500/20",
};

export default function DrawingSidebar() {
  const { t } = useTranslation();
  const open = useSelector(selectMenu);
  const supportModels = useSelector(selectSupportModels);
  const subscriptionData = useSelector(subscriptionDataSelector);
  const level = useSelector(levelSelector);
  const drawingModels = useMemo(
    () =>
      supportModels.filter((model) =>
        (model.tag ?? []).includes(DRAWING_TAG),
      ),
    [supportModels],
  );

  const [selectedId, setSelectedId] = useState<string>(
    drawingModels[0]?.id ?? "",
  );

  useEffect(() => {
    if (drawingModels.length === 0) {
      setSelectedId("");
      return;
    }

    if (!selectedId || !drawingModels.some((model) => model.id === selectedId)) {
      setSelectedId(drawingModels[0].id);
    }
  }, [drawingModels, selectedId]);

  const selectedModel =
    drawingModels.find((model) => model.id === selectedId) ?? null;

  const isPlanIncluded = useMemo(
    () => (modelId: string) =>
      subscriptionData
        ? includingModelFromPlan(subscriptionData, level, modelId)
        : false,
    [subscriptionData, level],
  );

  const getTagIcons = (modelId: string, tags: string[] = []) => {
    const mergedTags = [...(tags ?? [])];
    const planIncluded = isPlanIncluded(modelId);

    if (planIncluded && !mergedTags.includes(PLAN_INCLUDED_TAG)) {
      mergedTags.unshift(PLAN_INCLUDED_TAG);
    }

    return mergedTags
      .filter(
        (tag) =>
          TAG_ICON_MAP[tag] &&
          (tag === PLAN_INCLUDED_TAG || !HIDDEN_TAGS.includes(tag)),
      )
      .map((tag) => (
        <Tips
          key={`${modelId}-${tag}`}
          content={
            tag === PLAN_INCLUDED_TAG
              ? t("tag.badges.plan-included-tip")
              : t(`tag.${tag}`)
          }
          trigger={
            <span
              className={cn(
                "drawing-select-tag-icon drawing-tag-trigger bg-primary/5 ml-1",
                TAG_STYLE_MAP[tag] ?? "text-muted-foreground bg-primary/5",
              )}
            >
              <Icon icon={TAG_ICON_MAP[tag]} className="w-3.5 h-3.5" />
            </span>
          }
        />
      ));
  };

  return (
    <motion.div
      className={cn("sidebar drawing-sidebar", open && "open")}
      initial={{ opacity: 0, x: -24 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.35, ease: "easeOut" }}
    >
      <div className="drawing-sidebar-top">
        <div className="drawing-sidebar-header">
          <p className="drawing-sidebar-title">
            {t("drawing.modelSelectorTitle")}
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="drawing-history-button"
            disabled
          >
            <History className="w-4 h-4" />
            <span>{t("drawing.historyButton")}</span>
          </Button>
        </div>
        <Select
          value={selectedId}
          onValueChange={setSelectedId}
          disabled={drawingModels.length === 0}
        >
          <SelectTrigger className="drawing-model-select">
            {selectedModel ? (
              <SelectValue asChild>
                <div className="drawing-select-row">
                  <ModelAvatar model={selectedModel} size={24} />
                  <span className="drawing-select-name">
                    {selectedModel.name}
                  </span>
                </div>
              </SelectValue>
            ) : (
              <SelectValue placeholder={t("drawing.modelPlaceholder")} />
            )}
          </SelectTrigger>
          <SelectContent>
            {drawingModels.map((model) => (
              <SelectItem
                key={model.id}
                value={model.id}
                className="drawing-select-option"
              >
                <div className="drawing-select-row">
                  <ModelAvatar model={model} size={24} />
                  <span className="drawing-select-name">{model.name}</span>
                  <div className="drawing-select-tags">
                    {getTagIcons(model.id, model.tag)}
                  </div>
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {drawingModels.length === 0 && (
        <div className="drawing-empty-block">
          <p className="drawing-empty-title">{t("drawing.modelEmptyTitle")}</p>
          <p className="drawing-empty-desc">{t("drawing.modelEmptyDesc")}</p>
        </div>
      )}
    </motion.div>
  );
}
