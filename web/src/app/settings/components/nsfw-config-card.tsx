"use client";

import { LoaderCircle, Save, ShieldAlert } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

import { useSettingsStore } from "../store";
import { SettingsCard } from "./settings-ui";

export function NsfwConfigCard() {
  const config = useSettingsStore((state) => state.config);
  const isLoadingConfig = useSettingsStore((state) => state.isLoadingConfig);
  const isSavingConfig = useSettingsStore((state) => state.isSavingConfig);
  const setNsfwEnabled = useSettingsStore((state) => state.setNsfwEnabled);
  const saveConfig = useSettingsStore((state) => state.saveConfig);

  const enabled = config?.nsfw_enabled ?? true;

  if (isLoadingConfig) {
    return (
      <SettingsCard
        icon={ShieldAlert}
        title="NSFW 内容控制"
        description="控制提示词市场中 NSFW 筛选选项的可见性。"
        tone="violet"
      >
        <div className="flex items-center justify-center py-10">
          <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
        </div>
      </SettingsCard>
    );
  }

  return (
    <SettingsCard
      icon={ShieldAlert}
      title="NSFW 内容控制"
      description="关闭后，提示词市场仅允许隐藏 NSFW 内容，用户无法选择「包含 NSFW」或「仅 NSFW」。"
      tone="violet"
      action={
        <>
          <Badge variant={enabled ? "success" : "secondary"}>
            {enabled ? "NSFW 可选" : "NSFW 已禁用"}
          </Badge>
          <Button
            type="button"
            variant={enabled ? "outline" : "default"}
            onClick={() => setNsfwEnabled(!enabled)}
          >
            {enabled ? "禁用 NSFW" : "启用 NSFW"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          {enabled
            ? "当前用户可在提示词市场中选择「包含 NSFW」或「仅 NSFW」来浏览相关内容。"
            : "当前用户在提示词市场中只能看到「隐藏 NSFW」选项，无法浏览 NSFW 内容。"}
        </p>
        <div className="flex justify-end">
          <Button
            size="lg"
            onClick={() => void saveConfig()}
            disabled={isSavingConfig}
          >
            {isSavingConfig ? (
              <LoaderCircle data-icon="inline-start" className="animate-spin" />
            ) : (
              <Save data-icon="inline-start" />
            )}
            保存
          </Button>
        </div>
      </div>
    </SettingsCard>
  );
}
