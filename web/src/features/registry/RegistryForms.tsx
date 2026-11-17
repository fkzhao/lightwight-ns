import { Controller, useForm } from "react-hook-form"
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
import type { Instance, Machine, Service } from "@/types/registry"

interface OpenProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ServiceFormDialog({ open, onOpenChange, current }: OpenProps & { current?: Service }) {
  const { t } = useTranslation()
  const { save } = useRegistry()
  const schema = z.object({
    name: z.string().trim().min(1, t("validation.required")).max(80, t("validation.nameLength")),
    description: z.string().max(300, t("validation.descriptionLength")),
  })
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: current?.name ?? "", description: current?.description ?? "" },
  })
  const submit = form.handleSubmit(async (values) => {
    if (await save("services", values, current?.id)) onOpenChange(false)
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(current ? "services.edit" : "services.add")}</DialogTitle>
          <DialogDescription>{t("services.description")}</DialogDescription>
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

export function MachineFormDialog({ open, onOpenChange, current }: OpenProps & { current?: Machine }) {
  const { t } = useTranslation()
  const { save } = useRegistry()
  const schema = z.object({
    name: z.string().trim().min(1, t("validation.required")).max(80, t("validation.nameLength")),
    address: z.string().trim().min(1, t("validation.address")),
    description: z.string().max(300, t("validation.descriptionLength")),
  })
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: current?.name ?? "", address: current?.address ?? "", description: current?.description ?? "" },
  })
  const submit = form.handleSubmit(async (values) => {
    if (await save("machines", values, current?.id)) onOpenChange(false)
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(current ? "machines.edit" : "machines.add")}</DialogTitle>
          <DialogDescription>{t("machines.description")}</DialogDescription>
        </DialogHeader>
        <form id="machine-form" onSubmit={submit} className="flex flex-col gap-6">
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.name}>
              <FieldLabel htmlFor="machine-name">{t("machines.name")}</FieldLabel>
              <Input id="machine-name" placeholder={t("machines.namePlaceholder")} aria-invalid={!!form.formState.errors.name} {...form.register("name")} />
              <FieldError errors={[form.formState.errors.name]} />
            </Field>
            <Field data-invalid={!!form.formState.errors.address}>
              <FieldLabel htmlFor="machine-address">{t("machines.address")}</FieldLabel>
              <Input id="machine-address" placeholder={t("machines.addressPlaceholder")} aria-invalid={!!form.formState.errors.address} {...form.register("address")} />
              <FieldError errors={[form.formState.errors.address]} />
            </Field>
            <Field data-invalid={!!form.formState.errors.description}>
              <FieldLabel htmlFor="machine-description">{t("common.description")}</FieldLabel>
              <Textarea id="machine-description" aria-invalid={!!form.formState.errors.description} {...form.register("description")} />
              <FieldError errors={[form.formState.errors.description]} />
            </Field>
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
          <Button type="submit" form="machine-form" disabled={form.formState.isSubmitting}>{t("common.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function InstanceFormDialog({ open, onOpenChange, service, machines, current }: OpenProps & {
  service: Service
  machines: Machine[]
  current?: Instance
}) {
  const { t } = useTranslation()
  const { save } = useRegistry()
  const schema = z.object({
    machineId: z.string().min(1, t("validation.machine")),
    port: z.string().regex(/^\d+$/, t("validation.port")).refine((port) => Number(port) >= 1 && Number(port) <= 65535, t("validation.port")),
    enabled: z.boolean(),
  })
  type Values = z.infer<typeof schema>
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      machineId: current?.machineId ?? "",
      port: current?.port ? String(current.port) : "",
      enabled: current?.enabled ?? true,
    },
  })
  const submit = form.handleSubmit(async (values) => {
    const instance = { serviceId: service.id, machineId: values.machineId, port: Number(values.port), enabled: values.enabled }
    if (await save("instances", instance, current?.id)) onOpenChange(false)
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(current ? "services.editInstance" : "services.addInstance")}</DialogTitle>
          <DialogDescription>{service.name}</DialogDescription>
        </DialogHeader>
        <form id="instance-form" onSubmit={submit} className="flex flex-col gap-6">
          <FieldGroup>
            <Controller name="machineId" control={form.control} render={({ field, fieldState }) => (
              <Field data-invalid={fieldState.invalid}>
                <FieldLabel htmlFor="instance-machine">{t("instances.machine")}</FieldLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger id="instance-machine" className="w-full" aria-invalid={fieldState.invalid}>
                    <SelectValue placeholder={t("instances.selectMachine")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {machines.map((machine) => <SelectItem key={machine.id} value={machine.id}>{machine.name} · {machine.address}</SelectItem>)}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldError errors={[fieldState.error]} />
              </Field>
            )} />
            <Field data-invalid={!!form.formState.errors.port}>
              <FieldLabel htmlFor="instance-port">{t("instances.port")}</FieldLabel>
              <Input id="instance-port" inputMode="numeric" placeholder={t("instances.portPlaceholder")} aria-invalid={!!form.formState.errors.port} {...form.register("port")} />
              <FieldError errors={[form.formState.errors.port]} />
            </Field>
            <Controller name="enabled" control={form.control} render={({ field }) => (
              <Field orientation="horizontal">
                <FieldLabel htmlFor="instance-enabled">{t("instances.status")}</FieldLabel>
                <Switch id="instance-enabled" checked={field.value} onCheckedChange={field.onChange} />
                <FieldDescription>{t("instances.statusHelp")}</FieldDescription>
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

export function DeleteDialog({ open, onOpenChange, kind, id, description }: OpenProps & {
  kind: "services" | "machines" | "instances"
  id: string
  description: string
}) {
  const { t } = useTranslation()
  const { remove } = useRegistry()
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
            if (await remove(kind, id)) onOpenChange(false)
          }}>{t("common.delete")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
