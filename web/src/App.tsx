import { useState } from "react"
import { BrowserRouter, Link, NavLink, Navigate, Route, Routes, useParams } from "react-router"
import { useTranslation } from "react-i18next"
import { Activity, ArrowLeft, Boxes, Globe2, HardDrive, Menu, Moon, Pencil, Plus, Search, Server, Sun, Trash2, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { Separator } from "@/components/ui/separator"
import { Toaster } from "@/components/ui/sonner"
import { useTheme } from "@/components/theme-provider"
import { cn } from "@/lib/utils"
import { i18n } from "@/i18n"
import { RegistryProvider, useRegistry } from "@/features/registry/RegistryProvider"
import { DeleteDialog, InstanceFormDialog, MachineFormDialog, ServiceFormDialog } from "@/features/registry/RegistryForms"
import type { Instance, Machine, Service } from "@/types/registry"

function Navigation({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useTranslation()
  const items = [
    { to: "/", label: t("app.overview"), icon: Activity, end: true },
    { to: "/services", label: t("app.services"), icon: Boxes, end: false },
    { to: "/machines", label: t("app.machines"), icon: HardDrive, end: false },
  ]
  return (
    <nav className="flex flex-col gap-1" aria-label={t("app.menu")}>
      {items.map(({ to, label, icon: Icon, end }) => (
        <NavLink key={to} to={to} end={end} onClick={onNavigate}
          className={({ isActive }) => cn(
            "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground",
            isActive && "bg-accent font-medium text-accent-foreground"
          )}>
          <Icon className="size-4" />
          {label}
        </NavLink>
      ))}
    </nav>
  )
}

function AppShell() {
  const { t } = useTranslation()
  const { snapshot, loading, error, refresh } = useRegistry()
  const { theme, setTheme } = useTheme()
  const [menuOpen, setMenuOpen] = useState(false)
  const changeLanguage = () => {
    const next = i18n.language === "zh" ? "en" : "zh"
    localStorage.setItem("ns-language", next)
    void i18n.changeLanguage(next)
  }
  return (
    <div className="min-h-svh bg-background">
      <div className="mx-auto flex min-h-svh max-w-7xl">
        <aside className="hidden w-64 shrink-0 border-r border-border px-5 py-8 lg:flex lg:flex-col">
          <Link to="/" className="flex items-center gap-3 px-3">
            <span className="flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground"><Server className="size-5" /></span>
            <span className="flex flex-col">
              <span className="font-semibold leading-5">{t("app.title")}</span>
              <span className="text-xs text-muted-foreground">{t("app.subtitle")}</span>
            </span>
          </Link>
          <div className="mt-10"><Navigation /></div>
          <div className="mt-auto px-3 text-xs text-muted-foreground">{t("app.version")} {snapshot?.version ?? "—"}</div>
        </aside>
        <div className="min-w-0 flex-1">
          <header className="flex h-16 items-center justify-between border-b border-border px-4 sm:px-8">
            <div className="flex items-center gap-3">
              <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
                <SheetTrigger asChild>
                  <Button variant="ghost" size="icon" className="lg:hidden" aria-label={t("app.menu")}><Menu /></Button>
                </SheetTrigger>
                <SheetContent side="left" className="w-72">
                  <SheetHeader>
                    <SheetTitle>{t("app.title")}</SheetTitle>
                  </SheetHeader>
                  <div className="px-4 py-5"><Navigation onNavigate={() => setMenuOpen(false)} /></div>
                </SheetContent>
              </Sheet>
              <span className="font-medium lg:hidden">{t("app.title")}</span>
              <span className="hidden text-sm text-muted-foreground lg:inline">{t("app.subtitle")}</span>
            </div>
            <div className="flex items-center gap-1">
              <Button variant="ghost" size="sm" onClick={changeLanguage} aria-label={t("app.language")}>
                <Globe2 data-icon="inline-start" /> {t("app.languageAction")}
              </Button>
              <Button variant="ghost" size="icon" onClick={() => setTheme(theme === "dark" ? "light" : "dark")} aria-label={t("app.theme")}>
                {theme === "dark" ? <Sun /> : <Moon />}
              </Button>
            </div>
          </header>
          <main className="px-4 py-8 sm:px-8 sm:py-10">
            {loading && !snapshot ? <LoadingState /> : error && !snapshot ? (
              <Alert variant="destructive">
                <TriangleAlert />
                <AlertTitle>{t("errors.unknown")}</AlertTitle>
                <AlertDescription className="flex items-center gap-4">{error}<Button variant="outline" size="sm" onClick={() => void refresh()}>{t("common.retry")}</Button></AlertDescription>
              </Alert>
            ) : (
              <>
                {error && <Alert className="mb-6"><TriangleAlert /><AlertTitle>{error}</AlertTitle></Alert>}
                <Routes>
                  <Route path="/" element={<OverviewPage />} />
                  <Route path="/services" element={<ServicesPage />} />
                  <Route path="/services/:serviceId" element={<ServiceDetailPage />} />
                  <Route path="/machines" element={<MachinesPage />} />
                  <Route path="*" element={<Navigate to="/" replace />} />
                </Routes>
              </>
            )}
          </main>
        </div>
      </div>
      <Toaster />
    </div>
  )
}

function LoadingState() {
  return <div className="flex flex-col gap-6"><Skeleton className="h-9 w-64" /><Skeleton className="h-5 w-96 max-w-full" /><Skeleton className="h-72 w-full" /></div>
}

function PageHeading({ title, description, action }: { title: string; description: string; action?: React.ReactNode }) {
  return (
    <div className="mb-8 flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground">{description}</p>
      </div>
      {action}
    </div>
  )
}

function EmptyPanel({ icon: Icon, title, action }: { icon: typeof Server; title: string; action?: React.ReactNode }) {
  return <Empty className="min-h-64 border">
    <EmptyHeader>
      <EmptyMedia variant="icon"><Icon /></EmptyMedia>
      <EmptyTitle>{title}</EmptyTitle>
    </EmptyHeader>
    {action && <EmptyContent>{action}</EmptyContent>}
  </Empty>
}

function OverviewPage() {
  const { t } = useTranslation()
  const { snapshot } = useRegistry()
  if (!snapshot) return null
  const metrics = [
    { label: t("overview.services"), value: snapshot.services.length, icon: Boxes },
    { label: t("overview.machines"), value: snapshot.machines.length, icon: HardDrive },
    { label: t("overview.instances"), value: snapshot.instances.length, icon: Server },
    { label: t("overview.active"), value: snapshot.instances.filter((item) => item.enabled).length, icon: Activity },
  ]
  return (
    <>
      <PageHeading title={t("overview.title")} description={t("overview.description")} />
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {metrics.map(({ label, value, icon: Icon }) => (
          <Card key={label}>
            <CardHeader className="flex flex-row items-center justify-between gap-2">
              <CardDescription>{label}</CardDescription><Icon className="size-4 text-muted-foreground" />
            </CardHeader>
            <CardContent><p className="text-3xl font-semibold tracking-tight">{value}</p></CardContent>
          </Card>
        ))}
      </div>
      <Card className="mt-8">
        <CardHeader><CardTitle>{t("overview.latest")}</CardTitle><CardDescription>{t("services.description")}</CardDescription></CardHeader>
        <CardContent className="flex flex-col gap-1">
          {snapshot.services.length === 0 ? <p className="py-6 text-sm text-muted-foreground">{t("overview.empty")}</p> :
            snapshot.services.slice(-5).reverse().map((service) => (
              <Link key={service.id} to={"/services/" + service.id} className="flex items-center justify-between rounded-lg px-3 py-3 text-sm hover:bg-accent">
                <span className="font-medium">{service.name}</span>
                <Badge variant="secondary">{snapshot.instances.filter((item) => item.serviceId === service.id).length} {t("services.instances")}</Badge>
              </Link>
            ))}
          <Separator />
          <Button variant="link" asChild className="self-start"><Link to="/services">{t("app.services")}</Link></Button>
        </CardContent>
      </Card>
    </>
  )
}

function ServicesPage() {
  const { t } = useTranslation()
  const { snapshot } = useRegistry()
  const [query, setQuery] = useState("")
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Service | undefined>()
  const [deleting, setDeleting] = useState<Service | undefined>()
  if (!snapshot) return null
  const filtered = snapshot.services.filter((service) => service.name.toLowerCase().includes(query.toLowerCase()))
  const openNew = () => { setEditing(undefined); setFormOpen(true) }
  return <>
    <PageHeading title={t("services.title")} description={t("services.description")} action={<Button onClick={openNew}><Plus data-icon="inline-start" />{t("services.add")}</Button>} />
    <div className="mb-5 max-w-sm">
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input className="pl-9" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("common.search")} aria-label={t("common.search")} />
      </div>
    </div>
    {filtered.length === 0 ? <EmptyPanel icon={Boxes} title={t("services.empty")} action={<Button onClick={openNew}><Plus data-icon="inline-start" />{t("services.add")}</Button>} /> :
      <Card><CardContent className="overflow-x-auto p-0">
        <Table>
          <TableHeader><TableRow><TableHead>{t("services.name")}</TableHead><TableHead>{t("common.description")}</TableHead><TableHead>{t("services.instances")}</TableHead><TableHead className="text-right">{t("common.actions")}</TableHead></TableRow></TableHeader>
          <TableBody>{filtered.map((service) => (
            <TableRow key={service.id}>
              <TableCell><Link className="font-medium underline-offset-4 hover:underline" to={"/services/" + service.id}>{service.name}</Link></TableCell>
              <TableCell className="max-w-xs truncate text-muted-foreground">{service.description || t("common.none")}</TableCell>
              <TableCell><Badge variant="secondary">{snapshot.instances.filter((item) => item.serviceId === service.id).length}</Badge></TableCell>
              <TableCell><div className="flex justify-end gap-1">
                <Button variant="ghost" size="icon" aria-label={t("common.edit")} onClick={() => { setEditing(service); setFormOpen(true) }}><Pencil /></Button>
                <Button variant="ghost" size="icon" aria-label={t("common.delete")} onClick={() => setDeleting(service)}><Trash2 /></Button>
              </div></TableCell>
            </TableRow>
          ))}</TableBody>
        </Table>
      </CardContent></Card>}
    {formOpen && <ServiceFormDialog key={editing?.id ?? "new"} open={formOpen} onOpenChange={setFormOpen} current={editing} />}
    {deleting && <DeleteDialog open={!!deleting} onOpenChange={(open) => { if (!open) setDeleting(undefined) }} kind="services" id={deleting.id} description={t("services.deleteDescription")} />}
  </>
}

function MachinesPage() {
  const { t } = useTranslation()
  const { snapshot } = useRegistry()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Machine | undefined>()
  const [deleting, setDeleting] = useState<Machine | undefined>()
  if (!snapshot) return null
  const openNew = () => { setEditing(undefined); setFormOpen(true) }
  return <>
    <PageHeading title={t("machines.title")} description={t("machines.description")} action={<Button onClick={openNew}><Plus data-icon="inline-start" />{t("machines.add")}</Button>} />
    {snapshot.machines.length === 0 ? <EmptyPanel icon={HardDrive} title={t("machines.empty")} action={<Button onClick={openNew}><Plus data-icon="inline-start" />{t("machines.add")}</Button>} /> :
      <Card><CardContent className="overflow-x-auto p-0">
        <Table>
          <TableHeader><TableRow><TableHead>{t("machines.name")}</TableHead><TableHead>{t("machines.address")}</TableHead><TableHead>{t("machines.ports")}</TableHead><TableHead>{t("common.description")}</TableHead><TableHead className="text-right">{t("common.actions")}</TableHead></TableRow></TableHeader>
          <TableBody>{snapshot.machines.map((machine) => {
            const ports = snapshot.instances.filter((item) => item.machineId === machine.id).sort((a, b) => a.port - b.port)
            return <TableRow key={machine.id}>
              <TableCell className="font-medium">{machine.name}</TableCell>
              <TableCell className="font-mono text-xs">{machine.address}</TableCell>
              <TableCell><div className="flex flex-wrap gap-1">{ports.length ? ports.map((instance) => {
                const service = snapshot.services.find((item) => item.id === instance.serviceId)
                return <Link key={instance.id} to={"/services/" + instance.serviceId} title={service?.name}>
                  <Badge variant="secondary">{instance.port} · {service?.name}</Badge>
                </Link>
              }) : t("common.none")}</div></TableCell>
              <TableCell className="max-w-xs truncate text-muted-foreground">{machine.description || t("common.none")}</TableCell>
              <TableCell><div className="flex justify-end gap-1">
                <Button variant="ghost" size="icon" aria-label={t("common.edit")} onClick={() => { setEditing(machine); setFormOpen(true) }}><Pencil /></Button>
                <Button variant="ghost" size="icon" aria-label={t("common.delete")} onClick={() => setDeleting(machine)}><Trash2 /></Button>
              </div></TableCell>
            </TableRow>
          })}</TableBody>
        </Table>
      </CardContent></Card>}
    {formOpen && <MachineFormDialog key={editing?.id ?? "new"} open={formOpen} onOpenChange={setFormOpen} current={editing} />}
    {deleting && <DeleteDialog open={!!deleting} onOpenChange={(open) => { if (!open) setDeleting(undefined) }} kind="machines" id={deleting.id} description={t("machines.deleteDescription")} />}
  </>
}

function formatEndpoint(machine: Machine | undefined, port: number) {
  if (!machine) return "—"
  const address = machine.address.includes(":") ? "[" + machine.address + "]" : machine.address
  return address + ":" + port
}

function ServiceDetailPage() {
  const { t } = useTranslation()
  const { serviceId } = useParams()
  const { snapshot } = useRegistry()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Instance | undefined>()
  const [deleting, setDeleting] = useState<Instance | undefined>()
  if (!snapshot) return null
  const service = snapshot.services.find((item) => item.id === serviceId)
  if (!service) return <EmptyPanel icon={Boxes} title={t("services.notFound")} action={<Button variant="outline" asChild><Link to="/services">{t("app.services")}</Link></Button>} />
  const instances = snapshot.instances.filter((item) => item.serviceId === service.id)
  const openNew = () => { setEditing(undefined); setFormOpen(true) }
  return <>
    <Button variant="ghost" asChild className="mb-5"><Link to="/services"><ArrowLeft data-icon="inline-start" />{t("app.services")}</Link></Button>
    <PageHeading title={service.name} description={service.description || t("services.detailDescription")} action={<Button onClick={openNew} disabled={snapshot.machines.length === 0}><Plus data-icon="inline-start" />{t("services.addInstance")}</Button>} />
    {snapshot.machines.length === 0 && <Alert className="mb-6"><HardDrive /><AlertTitle>{t("instances.emptyMachines")}</AlertTitle></Alert>}
    {instances.length === 0 ? <EmptyPanel icon={Server} title={t("services.emptyInstances")} action={snapshot.machines.length > 0 ? <Button onClick={openNew}><Plus data-icon="inline-start" />{t("services.addInstance")}</Button> : undefined} /> :
      <Card>
        <CardHeader><CardTitle>{t("services.detail")}</CardTitle><CardDescription>{t("services.detailDescription")}</CardDescription></CardHeader>
        <CardContent className="overflow-x-auto p-0">
          <Table>
            <TableHeader><TableRow><TableHead>{t("instances.machine")}</TableHead><TableHead>{t("instances.address")}</TableHead><TableHead>{t("instances.status")}</TableHead><TableHead className="text-right">{t("common.actions")}</TableHead></TableRow></TableHeader>
            <TableBody>{instances.map((instance) => {
              const machine = snapshot.machines.find((item) => item.id === instance.machineId)
              return <TableRow key={instance.id}>
                <TableCell className="font-medium">{machine?.name ?? "—"}</TableCell>
                <TableCell className="font-mono text-xs">{formatEndpoint(machine, instance.port)}</TableCell>
                <TableCell><Badge variant={instance.enabled ? "secondary" : "outline"}>{t(instance.enabled ? "common.enabled" : "common.disabled")}</Badge></TableCell>
                <TableCell><div className="flex justify-end gap-1">
                  <Button variant="ghost" size="icon" aria-label={t("common.edit")} onClick={() => { setEditing(instance); setFormOpen(true) }}><Pencil /></Button>
                  <Button variant="ghost" size="icon" aria-label={t("common.delete")} onClick={() => setDeleting(instance)}><Trash2 /></Button>
                </div></TableCell>
              </TableRow>
            })}</TableBody>
          </Table>
        </CardContent>
      </Card>}
    {formOpen && <InstanceFormDialog key={editing?.id ?? "new"} open={formOpen} onOpenChange={setFormOpen} service={service} machines={snapshot.machines} current={editing} />}
    {deleting && <DeleteDialog open={!!deleting} onOpenChange={(open) => { if (!open) setDeleting(undefined) }} kind="instances" id={deleting.id} description={t("instances.deleteDescription")} />}
  </>
}

export function App() {
  return <BrowserRouter><RegistryProvider><AppShell /></RegistryProvider></BrowserRouter>
}

export default App
