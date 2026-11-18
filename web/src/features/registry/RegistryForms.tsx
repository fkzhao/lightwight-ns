import { Controller, useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useTranslation } from "react-i18next"
import { z } from "zod"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useRegistry } from "./RegistryProvider"
import type { Instance, Service } from "@/types/registry"

interface OpenProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ServiceFormDialog({ open, onOpenChange, current }: OpenProps & { current?: Service }) {
  const { t } = useTranslation()
  const { saveService } = useRegistry()
  const schema = z.object({
    name: z.string().trim().min(1, t("validation.required")).max(80, t("validation.nameLength")),
    description: z.string().max(300, t("validation.descriptionLength")),
    namespace: z.string().trim().min(1, t("validation.required")).max(80),
    group: z.string().trim().min(1, t("validation.required")).max(80),
    healthType: z.enum(["tcp", "http", "grpc"]),
    failureThreshold: z.string().refine((v) => /^\d+$/.test(v) && +v >= 1 && +v <= 20, t("validation.threshold")),
    successThreshold: z.string().refine((v) => /^\d+$/.test(v) && +v >= 1 && +v <= 20, t("validation.threshold")),
    intervalMillis: z.string().refine((v) => /^\d+$/.test(v) && +v <= 3600000, t("validation.interval")),
    timeoutMillis: z.string().refine((v) => /^\d+$/.test(v) && +v <= 60000, t("validation.timeout")),
    grpcService: z.string().max(200), tls: z.boolean(), serverName: z.string().max(200), caPem: z.string().max(65536),
    protectionEnabled: z.boolean(), allowStale: z.boolean(),
    minInstances: z.string().refine((v) => /^\d+$/.test(v) && +v >= 2 && +v <= 100000, t("validation.protection")),
    renewalThreshold: z.string().refine((v) => +v > 0 && +v <= 1, t("validation.protection")),
    maxHoldSeconds: z.string().refine((v) => /^\d+$/.test(v) && +v >= 10 && +v <= 600, t("validation.protection")),
    maxStaleSeconds: z.string().refine((v) => /^\d+$/.test(v) && +v >= 1 && +v <= 300, t("validation.protection")),
    healthPath: z.string().max(200, t("validation.healthPath")),
  }).superRefine((values, ctx) => {
    if (values.healthType === "http" && (!values.healthPath.startsWith("/") || values.healthPath.startsWith("//"))) {
      ctx.addIssue({ code: "custom", path: ["healthPath"], message: t("validation.healthPath") })
    }
  })
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: current?.name ?? "",
      description: current?.description ?? "",
      namespace: current?.namespace ?? "default", group: current?.group ?? "default",
      healthType: current?.healthCheck.type ?? "tcp",
      failureThreshold: String(current?.healthCheck.failureThreshold ?? 3), successThreshold: String(current?.healthCheck.successThreshold ?? 2),
      intervalMillis: String(current?.healthCheck.intervalMillis ?? 0), timeoutMillis: String(current?.healthCheck.timeoutMillis ?? 0),
      grpcService: current?.healthCheck.grpcService ?? "", tls: current?.healthCheck.tls ?? false,
      serverName: current?.healthCheck.serverName ?? "", caPem: current?.healthCheck.caPem ?? "",
      protectionEnabled: current?.protection.enabled ?? false, allowStale: current?.protection.allowStale ?? false,
      minInstances: String(current?.protection.minInstances ?? 10), renewalThreshold: String(current?.protection.renewalThreshold ?? .85),
      maxHoldSeconds: String(current?.protection.maxHoldSeconds ?? 120), maxStaleSeconds: String(current?.protection.maxStaleSeconds ?? 60),
      healthPath: current?.healthCheck.path || "/health",
    },
  })
  const healthType = useWatch({ control: form.control, name: "healthType" })
  const submit = form.handleSubmit(async (values) => {
    const saved = await saveService({
      name: values.name, namespace: values.namespace, group: values.group,
      description: values.description,
      healthCheck: { type: values.healthType, path: values.healthType === "http" ? values.healthPath : "",
        failureThreshold: +values.failureThreshold, successThreshold: +values.successThreshold,
        intervalMillis: +values.intervalMillis, timeoutMillis: +values.timeoutMillis, grpcService: values.grpcService,
        tls: values.tls, serverName: values.serverName, caPem: values.caPem },
      protection: { ...current?.protection, enabled: values.protectionEnabled, allowStale: values.allowStale,
        minInstances: +values.minInstances, renewalThreshold: +values.renewalThreshold,
        maxHoldSeconds: +values.maxHoldSeconds, maxStaleSeconds: +values.maxStaleSeconds },
    }, current?.id)
    if (saved) onOpenChange(false)
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85svh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t(current ? "services.edit" : "services.add")}</DialogTitle>
          <DialogDescription>{t("services.formHelp")}</DialogDescription>
        </DialogHeader>
        <form id="service-form" onSubmit={submit} className="flex flex-col gap-6">
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.name}>
              <FieldLabel htmlFor="service-name">{t("services.name")}</FieldLabel>
              <Input id="service-name" placeholder={t("services.namePlaceholder")} aria-invalid={!!form.formState.errors.name} {...form.register("name")} />
              <FieldError errors={[form.formState.errors.name]} />
            </Field>
            <Field data-invalid={!!form.formState.errors.description}>
              <FieldLabel htmlFor="service-description">{t("common.description")}</FieldLabel>
              <Textarea id="service-description" aria-invalid={!!form.formState.errors.description} {...form.register("description")} />
              <FieldError errors={[form.formState.errors.description]} />
            </Field>
            {(["namespace", "group"] as const).map((name) => <Field key={name} data-invalid={!!form.formState.errors[name]}>
              <FieldLabel htmlFor={"service-" + name}>{t("services." + name)}</FieldLabel>
              <Input id={"service-" + name} aria-invalid={!!form.formState.errors[name]} {...form.register(name)} />
              <FieldError errors={[form.formState.errors[name]]} />
            </Field>)}
            <Controller name="healthType" control={form.control} render={({ field }) => (
              <Field>
                <FieldLabel htmlFor="health-type">{t("health.type")}</FieldLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="health-type" className="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>
                    <SelectItem value="tcp">{t("health.tcp")}</SelectItem>
                    <SelectItem value="http">{t("health.http")}</SelectItem>
                    <SelectItem value="grpc">{t("health.grpc")}</SelectItem>
                  </SelectGroup></SelectContent>
                </Select>
                <FieldDescription>{t("health.typeHelp")}</FieldDescription>
              </Field>
            )} />
            {healthType === "http" && <Field data-invalid={!!form.formState.errors.healthPath}>
              <FieldLabel htmlFor="health-path">{t("health.path")}</FieldLabel>
              <Input id="health-path" placeholder="/health" aria-invalid={!!form.formState.errors.healthPath} {...form.register("healthPath")} />
              <FieldError errors={[form.formState.errors.healthPath]} />
            </Field>}
            {(["failureThreshold", "successThreshold", "intervalMillis", "timeoutMillis"] as const).map((name) => <Field key={name} data-invalid={!!form.formState.errors[name]}>
              <FieldLabel htmlFor={name}>{t("health." + name)}</FieldLabel>
              <Input id={name} inputMode="numeric" aria-invalid={!!form.formState.errors[name]} {...form.register(name)} />
              <FieldError errors={[form.formState.errors[name]]} />
            </Field>)}
            <FieldDescription>{t("health.timingHelp")}</FieldDescription>
            {healthType === "grpc" && <>
              {(["grpcService", "serverName"] as const).map((name) => <Field key={name}>
                <FieldLabel htmlFor={name}>{t("health." + name)}</FieldLabel><Input id={name} {...form.register(name)} />
              </Field>)}
              <Controller name="tls" control={form.control} render={({ field }) => <Field orientation="horizontal"><FieldLabel htmlFor="health-tls">{t("health.tls")}</FieldLabel><Switch id="health-tls" checked={field.value} onCheckedChange={field.onChange} /></Field>} />
              <Field><FieldLabel htmlFor="caPem">{t("health.caPem")}</FieldLabel><Textarea id="caPem" {...form.register("caPem")} /></Field>
            </>}
            <details><summary className="cursor-pointer">{t("protection.title")}</summary><FieldGroup className="mt-4">
              <Controller name="protectionEnabled" control={form.control} render={({ field }) => <Field orientation="horizontal"><FieldLabel htmlFor="protection-enabled">{t("protection.enabled")}</FieldLabel><Switch id="protection-enabled" checked={field.value} onCheckedChange={field.onChange} /></Field>} />
              <FieldDescription>{t("protection.help")}</FieldDescription>
              {(["minInstances", "renewalThreshold", "maxHoldSeconds", "maxStaleSeconds"] as const).map((name) => <Field key={name} data-invalid={!!form.formState.errors[name]}>
                <FieldLabel htmlFor={name}>{t("protection." + name)}</FieldLabel><Input id={name} aria-invalid={!!form.formState.errors[name]} {...form.register(name)} /><FieldError errors={[form.formState.errors[name]]} />
              </Field>)}
              <Controller name="allowStale" control={form.control} render={({ field }) => <Field orientation="horizontal"><FieldLabel htmlFor="allow-stale">{t("protection.allowStale")}</FieldLabel><Switch id="allow-stale" checked={field.value} onCheckedChange={field.onChange} /></Field>} />
            </FieldGroup></details>
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
          <Button type="submit" form="service-form" disabled={form.formState.isSubmitting}>{t("common.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function InstanceFormDialog({ open, onOpenChange, service, current }: OpenProps & {
  service: Service
  current?: Instance
}) {
  const { t } = useTranslation()
  const { saveInstance } = useRegistry()
  const schema = z.object({
    address: z.string().trim().min(1, t("validation.address")),
    port: z.string().regex(/^\d+$/, t("validation.port")).refine((port) => Number(port) >= 1 && Number(port) <= 65535, t("validation.port")),
    enabled: z.boolean(),
    version: z.string().max(80), weight: z.string().refine((v) => /^\d+$/.test(v) && +v <= 10000, t("validation.weight")),
    metadata: z.string().refine((v) => { try { return z.record(z.string(), z.string()).safeParse(JSON.parse(v)).success } catch { return false } }, t("validation.metadata")),
  })
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      address: current?.address ?? "",
      port: current?.port ? String(current.port) : "",
      enabled: current?.enabled ?? true, version: current?.version ?? "", weight: String(current?.weight ?? 100), metadata: JSON.stringify(current?.metadata ?? {}, null, 2),
    },
  })
  const submit = form.handleSubmit(async (values) => {
    if (await saveInstance(service.id, { address: values.address, port: Number(values.port), enabled: values.enabled, version: values.version, weight: +values.weight, metadata: JSON.parse(values.metadata) as Record<string, string>, lifecycle: current?.lifecycle ?? "active" }, current?.id)) {
      onOpenChange(false)
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85svh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t(current ? "instances.edit" : "instances.add")}</DialogTitle>
          <DialogDescription>{service.name}</DialogDescription>
        </DialogHeader>
        <form id="instance-form" onSubmit={submit} className="flex flex-col gap-6">
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.address}>
              <FieldLabel htmlFor="instance-address">{t("instances.address")}</FieldLabel>
              <Input id="instance-address" placeholder={t("instances.addressPlaceholder")} aria-invalid={!!form.formState.errors.address} {...form.register("address")} />
              <FieldError errors={[form.formState.errors.address]} />
            </Field>
            <Field data-invalid={!!form.formState.errors.port}>
              <FieldLabel htmlFor="instance-port">{t("instances.port")}</FieldLabel>
              <Input id="instance-port" inputMode="numeric" placeholder={t("instances.portPlaceholder")} aria-invalid={!!form.formState.errors.port} {...form.register("port")} />
              <FieldError errors={[form.formState.errors.port]} />
            </Field>
            {(["version", "weight"] as const).map((name) => <Field key={name} data-invalid={!!form.formState.errors[name]}>
              <FieldLabel htmlFor={"instance-" + name}>{t("instances." + name)}</FieldLabel><Input id={"instance-" + name} aria-invalid={!!form.formState.errors[name]} {...form.register(name)} /><FieldError errors={[form.formState.errors[name]]} />
            </Field>)}
            <Field data-invalid={!!form.formState.errors.metadata}><FieldLabel htmlFor="instance-metadata">{t("instances.metadata")}</FieldLabel><Textarea id="instance-metadata" aria-invalid={!!form.formState.errors.metadata} {...form.register("metadata")} /><FieldError errors={[form.formState.errors.metadata]} /></Field>
            <Controller name="enabled" control={form.control} render={({ field }) => (
              <Field orientation="horizontal">
                <FieldLabel htmlFor="instance-enabled">{t("instances.enabled")}</FieldLabel>
                <Switch id="instance-enabled" checked={field.value} onCheckedChange={field.onChange} />
                <FieldDescription>{t("instances.enabledHelp")}</FieldDescription>
              </Field>
            )} />
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
          <Button type="submit" form="instance-form" disabled={form.formState.isSubmitting}>{t("common.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function DeleteDialog({ open, onOpenChange, serviceId, instanceId, description }: OpenProps & {
  serviceId: string
  instanceId?: string
  description: string
}) {
  const { t } = useTranslation()
  const { removeService, removeInstance } = useRegistry()
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("common.confirmDelete")}</AlertDialogTitle>
          <AlertDialogDescription>{description} {t("common.deleteWarning")}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={async (event) => {
            event.preventDefault()
            const removed = instanceId ? await removeInstance(serviceId, instanceId) : await removeService(serviceId)
            if (removed) onOpenChange(false)
          }}>{t("common.delete")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

export function DrainDialog({ open, onOpenChange, serviceId, instanceId }: OpenProps & { serviceId: string; instanceId: string }) {
  const { t } = useTranslation()
  const { setInstanceState } = useRegistry()
  return <AlertDialog open={open} onOpenChange={onOpenChange}><AlertDialogContent>
    <AlertDialogHeader><AlertDialogTitle>{t("instances.drain")}</AlertDialogTitle><AlertDialogDescription>{t("instances.drainHelp")}</AlertDialogDescription></AlertDialogHeader>
    <AlertDialogFooter><AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel><AlertDialogAction onClick={async (event) => {
      event.preventDefault()
      if (await setInstanceState(serviceId, instanceId, "drain")) onOpenChange(false)
    }}>{t("instances.drain")}</AlertDialogAction></AlertDialogFooter>
  </AlertDialogContent></AlertDialog>
}
