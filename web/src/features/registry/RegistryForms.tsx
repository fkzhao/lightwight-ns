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
    healthType: z.enum(["tcp", "http"]),
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
      healthType: current?.healthCheck.type ?? "tcp",
      healthPath: current?.healthCheck.path || "/health",
    },
  })
  const healthType = useWatch({ control: form.control, name: "healthType" })
  const submit = form.handleSubmit(async (values) => {
    const saved = await saveService({
      name: values.name,
      description: values.description,
      healthCheck: { type: values.healthType, path: values.healthType === "http" ? values.healthPath : "" },
    }, current?.id)
    if (saved) onOpenChange(false)
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
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
            <Controller name="healthType" control={form.control} render={({ field }) => (
              <Field>
                <FieldLabel htmlFor="health-type">{t("health.type")}</FieldLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="health-type" className="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>
                    <SelectItem value="tcp">{t("health.tcp")}</SelectItem>
                    <SelectItem value="http">{t("health.http")}</SelectItem>
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
  })
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      address: current?.address ?? "",
      port: current?.port ? String(current.port) : "",
      enabled: current?.enabled ?? true,
    },
  })
  const submit = form.handleSubmit(async (values) => {
    if (await saveInstance(service.id, { address: values.address, port: Number(values.port), enabled: values.enabled }, current?.id)) {
      onOpenChange(false)
    }
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
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
