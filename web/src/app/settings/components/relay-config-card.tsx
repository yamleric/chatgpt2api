"use client";

import { LoaderCircle, Save, Server } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

import { useSettingsStore } from "../store";
import { SettingsCard, settingsDialogInputClassName } from "./settings-ui";

const sectionClassName = "flex flex-col gap-3";
const fieldClassName = "gap-1.5";

export function RelayConfigCard() {
  const config = useSettingsStore((state) => state.config);
  const isLoadingConfig = useSettingsStore((state) => state.isLoadingConfig);
  const isSavingConfig = useSettingsStore((state) => state.isSavingConfig);
  const setRelayEnabled = useSettingsStore((state) => state.setRelayEnabled);
  const setRelayBaseUrl = useSettingsStore((state) => state.setRelayBaseUrl);
  const setRelayApiKey = useSettingsStore((state) => state.setRelayApiKey);
  const setRelayModel = useSettingsStore((state) => state.setRelayModel);
  const setRelayTimeoutSeconds = useSettingsStore(
    (state) => state.setRelayTimeoutSeconds,
  );
  const saveConfig = useSettingsStore((state) => state.saveConfig);

  const enabled = Boolean(config?.relay_enabled);
  const apiKeyConfigured = Boolean(config?.relay_api_key_configured);

  if (isLoadingConfig) {
    return (
      <SettingsCard
        icon={Server}
        title="中转站配置"
        description="启用后，图像生成/编辑请求将转发到 OpenAI 兼容中转站，不再使用本地账号池。"
        tone="amber"
      >
        <div className="flex items-center justify-center py-10">
          <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
        </div>
      </SettingsCard>
    );
  }

  return (
    <SettingsCard
      icon={Server}
      title="中转站配置"
      description="启用后，图像生成/编辑请求将转发到 OpenAI 兼容中转站，不再使用本地账号池。"
      tone="amber"
      action={
        <>
          <Badge variant={enabled ? "success" : "secondary"}>
            {enabled ? "中转站已启用" : "中转站已关闭"}
          </Badge>
          <Button
            type="button"
            variant={enabled ? "outline" : "default"}
            onClick={() => setRelayEnabled(!enabled)}
          >
            {enabled ? "关闭中转站" : "启用中转站"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <section className={sectionClassName}>
          <h3 className="truncate text-sm leading-6 font-semibold text-foreground">
            连接信息
          </h3>
          <div className="grid gap-3">
            <Field className={fieldClassName}>
              <FieldLabel htmlFor="relay-base-url">中转站地址</FieldLabel>
              <Input
                id="relay-base-url"
                value={String(config?.relay_base_url || "")}
                onChange={(event) => setRelayBaseUrl(event.target.value)}
                placeholder="https://your-relay.example.com"
                className={`${settingsDialogInputClassName} font-mono text-sm`}
              />
              <p className="text-xs text-muted-foreground">
                不含尾部斜杠；示例：<span className="font-mono">https://your-relay.example.com</span>
              </p>
            </Field>

            <Field className={fieldClassName}>
              <FieldLabel htmlFor="relay-api-key">API Key</FieldLabel>
              <Input
                id="relay-api-key"
                type="password"
                value={String(config?.relay_api_key || "")}
                onChange={(event) => setRelayApiKey(event.target.value)}
                placeholder={
                  apiKeyConfigured
                    ? "已配置，留空则保留当前密钥"
                    : "sk-xxxxxxxx"
                }
                className={`${settingsDialogInputClassName} font-mono text-sm`}
              />
            </Field>
          </div>
        </section>

        <section className={sectionClassName}>
          <h3 className="truncate text-sm leading-6 font-semibold text-foreground">
            可选参数
          </h3>
          <div className="grid items-start gap-3 md:grid-cols-2">
            <Field className={fieldClassName}>
              <FieldLabel htmlFor="relay-model">强制模型</FieldLabel>
              <Input
                id="relay-model"
                value={String(config?.relay_model || "")}
                onChange={(event) => setRelayModel(event.target.value)}
                placeholder="留空则自动映射"
                className={settingsDialogInputClassName}
              />
              <p className="text-xs text-muted-foreground">
                强制覆盖请求模型名
              </p>
            </Field>

            <Field className={fieldClassName}>
              <FieldLabel htmlFor="relay-timeout-seconds">请求超时（秒）</FieldLabel>
              <Input
                id="relay-timeout-seconds"
                type="number"
                min={10}
                max={600}
                value={String(config?.relay_timeout_seconds ?? 300)}
                onChange={(event) =>
                  setRelayTimeoutSeconds(event.target.value)
                }
                className={settingsDialogInputClassName}
              />
              <p className="text-xs text-muted-foreground">
                范围 10–600，默认 300
              </p>
            </Field>
          </div>
        </section>

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
