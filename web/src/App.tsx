import { useState } from "react"
import { BrowserRouter, Navigate, Route, Routes } from "react-router"
import { useTranslation } from "react-i18next"
import { Activity, LogOut, Power, Globe2, Moon, Pencil, Plus, Search, Server, Sun, Trash2, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { Toaster } from "@/components/ui/sonner"
import { useTheme } from "@/components/theme-provider"
import { i18n } from "@/i18n"
import { RegistryProvider, useRegistry } from "@/features/registry/RegistryProvider"
import { DeleteDialog, DrainDialog, InstanceFormDialog, ServiceFormDialog } from "@/features/registry/RegistryForms"
import type { Instance, Service } from "@/types/registry"

function HealthBadge({ instance }: { instance: Instance }) {
  const { t } = useTranslation()
  if (!instance.enabled) return <Badge variant="outline">{t("health.disabled")}</Badge>
  if (instance.lifecycle === "draining") return <Badge variant="outline">{t("health.draining")}</Badge>
  if (instance.leaseExpired) return <Badge variant="destructive">{t("health.expired")}</Badge>
  if (instance.weight === 0) return <Badge variant="outline">{t("health.zeroWeight")}</Badge>
  if (instance.health === "healthy") return <Badge variant="success">{t("health.healthy")}</Badge>
  if (instance.health === "unhealthy") return <Badge variant="destructive">{t("health.unhealthy")}</Badge>
  return <Badge variant="outline">{t("health.unknown")}</Badge>
}

function ServiceCard({ service }: { service: Service }) {
  const { t } = useTranslation()
  const [serviceFormOpen, setServiceFormOpen] = useState(false)
  const [instanceFormOpen, setInstanceFormOpen] = useState(false)
  const [editingInstance, setEditingInstance] = useState<Instance | undefined>()
  const [deleteServiceOpen, setDeleteServiceOpen] = useState(false)
  const { setInstanceState } = useRegistry()
  const [drainingInstance, setDrainingInstance] = useState<Instance | undefined>()
  const [deletingInstance, setDeletingInstance] = useState<Instance | undefined>()
  const available = service.instances.filter((instance) => instance.enabled && instance.health === "healthy" && instance.lifecycle !== "draining" && !instance.leaseExpired && instance.weight !== 0).length

  return <Card>
    <CardHeader className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-3">
          <CardTitle className="text-lg">{service.name}</CardTitle>
          <Badge variant="outline">{t("health." + service.healthCheck.type)}{service.healthCheck.type === "http" ? " " + service.healthCheck.path : ""}</Badge>
        </div>
        <p className="text-xs text-muted-foreground">{t("services.namespace")}: {service.namespace} · {t("services.group")}: {service.group}</p>
        <CardDescription>{service.description || t("services.noDescription")}</CardDescription>
        <p className="text-xs text-muted-foreground">{t("services.availability", { available, total: service.instances.length })}</p>
      </div>
      <div className="flex shrink-0 gap-1">
        <Button variant="ghost" size="icon" aria-label={t("services.edit")} onClick={() => setServiceFormOpen(true)}><Pencil /></Button>
        <Button variant="ghost" size="icon" aria-label={t("services.delete")} onClick={() => setDeleteServiceOpen(true)}><Trash2 /></Button>
      </div>
    </CardHeader>
    <CardContent>
      {service.protectionStatus?.active && <Alert className="mb-4"><TriangleAlert /><AlertTitle>{t("protection.active")}</AlertTitle><AlertDescription>{t("protection." + (service.protectionStatus.reason ?? "renewal_drop"))} {t("protection.activeHelp")}</AlertDescription></Alert>}

      <div className="mb-4 flex items-center gap-2">
        <Server className="size-4 text-muted-foreground" />
        <h3 className="text-sm font-medium">{t("instances.title")}</h3>
      </div>
      {service.instances.length === 0 ?
        <Empty className="min-h-40 border border-dashed">
          <EmptyHeader>
            <EmptyMedia variant="icon"><Server /></EmptyMedia>
            <EmptyTitle>{t("instances.empty")}</EmptyTitle>
            <EmptyDescription>{t("instances.emptyHelp")}</EmptyDescription>
          </EmptyHeader>
        </Empty> :
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader><TableRow>
              <TableHead>{t("instances.address")}</TableHead>
              <TableHead>{t("instances.port")}</TableHead>
              <TableHead>{t("instances.metadata")}</TableHead>
              <TableHead>{t("instances.health")}</TableHead>
              <TableHead className="text-right">{t("common.actions")}</TableHead>
            </TableRow></TableHeader>
            <TableBody>{service.instances.map((instance) => <TableRow key={instance.id}>
              <TableCell className="font-mono text-xs"><div className="flex items-center gap-2">{instance.address}{instance.source === "sdk" && <Badge variant="outline">{t("instances.sdkManaged")}</Badge>}</div></TableCell>
              <TableCell className="font-mono text-xs">{instance.port}</TableCell>
              <TableCell><div className="flex flex-col gap-1 text-xs"><span>{t("instances.version")}: {instance.version || t("instances.unspecified")}</span><span>{t("instances.weight")}: {instance.weight ?? 100}</span>{instance.metadata && Object.keys(instance.metadata).length > 0 && <code className="max-w-48 truncate" title={JSON.stringify(instance.metadata)}>{JSON.stringify(instance.metadata)}</code>}</div></TableCell>
              <TableCell><div className="flex max-w-64 flex-col gap-1"><HealthBadge instance={instance} />{instance.lastError && <p className="truncate text-xs text-muted-foreground" title={instance.lastError}>{instance.lastError}</p>}{instance.lastCheckAt && <span className="text-xs text-muted-foreground">{t("health.checkedAt")}: {new Date(instance.lastCheckAt).toLocaleTimeString(i18n.language)}</span>}</div></TableCell>
              <TableCell><div className="flex justify-end gap-1">
                <Button variant="ghost" size="icon" aria-label={t(instance.enabled ? "instances.disable" : "instances.enable")} onClick={() => void setInstanceState(service.id, instance.id, "enabled", !instance.enabled)}><Power /></Button>
                {instance.lifecycle !== "draining" && <Button variant="ghost" size="icon" aria-label={t("instances.drain")} onClick={() => setDrainingInstance(instance)}><LogOut /></Button>}
                {instance.source !== "sdk" && <Button variant="ghost" size="icon" aria-label={t("instances.edit")} onClick={() => { setEditingInstance(instance); setInstanceFormOpen(true) }}><Pencil /></Button>}
                {instance.source !== "sdk" && <Button variant="ghost" size="icon" aria-label={t("instances.delete")} onClick={() => setDeletingInstance(instance)}><Trash2 /></Button>}
              </div></TableCell>
            </TableRow>)}</TableBody>
          </Table>
        </div>}
    </CardContent>
    <CardFooter>
      <Button variant="outline" size="sm" onClick={() => { setEditingInstance(undefined); setInstanceFormOpen(true) }}>
        <Plus data-icon="inline-start" />{t("instances.add")}
      </Button>
    </CardFooter>
    {serviceFormOpen && <ServiceFormDialog open={serviceFormOpen} onOpenChange={setServiceFormOpen} current={service} />}
    {instanceFormOpen && <InstanceFormDialog key={editingInstance?.id ?? "new"} open={instanceFormOpen} onOpenChange={setInstanceFormOpen} service={service} current={editingInstance} />}
    {deleteServiceOpen && <DeleteDialog open={deleteServiceOpen} onOpenChange={setDeleteServiceOpen} serviceId={service.id} description={t("services.deleteHelp")} />}
    {drainingInstance && <DrainDialog open={!!drainingInstance} onOpenChange={(open) => { if (!open) setDrainingInstance(undefined) }} serviceId={service.id} instanceId={drainingInstance.id} />}
    {deletingInstance && <DeleteDialog open={!!deletingInstance} onOpenChange={(open) => { if (!open) setDeletingInstance(undefined) }} serviceId={service.id} instanceId={deletingInstance.id} description={t("instances.deleteHelp")} />}
  </Card>
}

function RegistryPage() {
  const { t } = useTranslation()
  const { snapshot, loading, error, refresh } = useRegistry()
  const { theme, setTheme } = useTheme()
  const [query, setQuery] = useState("")
  const [serviceFormOpen, setServiceFormOpen] = useState(false)
  const changeLanguage = () => {
    const next = i18n.language === "zh" ? "en" : "zh"
    localStorage.setItem("ns-language", next)
    void i18n.changeLanguage(next)
  }
  const filtered = snapshot?.services.filter((service) => [service.name, service.namespace, service.group].join(" ").toLowerCase().includes(query.toLowerCase())) ?? []
  const available = snapshot?.services.reduce((sum, service) => sum + service.instances.filter((item) => item.enabled && item.health === "healthy" && item.lifecycle !== "draining" && !item.leaseExpired && item.weight !== 0).length, 0) ?? 0
  return <div className="min-h-svh bg-background">
    <header className="border-b border-border">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-4 sm:px-6">
        <div className="flex items-center gap-3">
          <span className="flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground"><Server className="size-5" /></span>
          <div className="flex flex-col"><span className="font-semibold leading-5">{t("app.title")}</span><span className="text-xs text-muted-foreground">{t("app.subtitle")}</span></div>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="sm" onClick={changeLanguage} aria-label={t("app.language")}><Globe2 data-icon="inline-start" />{t("app.languageAction")}</Button>
          <Button variant="ghost" size="icon" onClick={() => setTheme(theme === "dark" ? "light" : "dark")} aria-label={t("app.theme")}>{theme === "dark" ? <Sun /> : <Moon />}</Button>
        </div>
      </div>
    </header>
    <main className="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <div className="mb-8 flex flex-col justify-between gap-5 sm:flex-row sm:items-start">
        <div className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold tracking-tight">{t("services.title")}</h1>
          <p className="max-w-2xl text-sm leading-6 text-muted-foreground">{t("services.description")}</p>
        </div>
        <Button onClick={() => setServiceFormOpen(true)}><Plus data-icon="inline-start" />{t("services.add")}</Button>
      </div>
      {snapshot && <div className="mb-8 flex flex-wrap items-center gap-5 border-y border-border py-4 text-sm text-muted-foreground">
        <span>{t("services.count", { count: snapshot.services.length })}</span>
        <span className="flex items-center gap-2"><Activity className="size-4" />{t("services.availableCount", { count: available })}</span>
        <span>{t("app.version")} {snapshot.version}</span>
      </div>}
      {loading && !snapshot ? <div className="flex flex-col gap-5"><Skeleton className="h-10 w-64" /><Skeleton className="h-64 w-full" /></div> :
        error && !snapshot ? <Alert variant="destructive"><TriangleAlert /><AlertTitle>{t("errors.unknown")}</AlertTitle><AlertDescription className="flex items-center gap-4">{error}<Button variant="outline" size="sm" onClick={() => void refresh()}>{t("common.retry")}</Button></AlertDescription></Alert> :
        <>
          {error && <Alert className="mb-6"><TriangleAlert /><AlertTitle>{error}</AlertTitle></Alert>}
          {(snapshot?.services.length ?? 0) > 0 && <div className="relative mb-6 max-w-sm">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input className="pl-9" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("services.search")} aria-label={t("services.search")} />
          </div>}
          {filtered.length === 0 ? <Empty className="min-h-72 border border-dashed">
            <EmptyHeader><EmptyMedia variant="icon"><Server /></EmptyMedia><EmptyTitle>{t("services.empty")}</EmptyTitle><EmptyDescription>{t("services.emptyHelp")}</EmptyDescription></EmptyHeader>
            <EmptyContent><Button onClick={() => setServiceFormOpen(true)}><Plus data-icon="inline-start" />{t("services.add")}</Button></EmptyContent>
          </Empty> : <div className="flex flex-col gap-6">{filtered.map((service) => <ServiceCard key={service.id} service={service} />)}</div>}
        </>}
    </main>
    {serviceFormOpen && <ServiceFormDialog open={serviceFormOpen} onOpenChange={setServiceFormOpen} />}
    <Toaster />
  </div>
}

export function App() {
  return <BrowserRouter><RegistryProvider><Routes>
    <Route path="/services" element={<RegistryPage />} />
    <Route path="/" element={<Navigate to="/services" replace />} />
    <Route path="*" element={<Navigate to="/services" replace />} />
  </Routes></RegistryProvider></BrowserRouter>
}

export default App
