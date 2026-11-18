import { createContext, useCallback, useContext, useEffect, useState } from "react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api, ApiError } from "@/lib/api"
import type { InstanceInput, ServiceInput } from "@/lib/api"
import type { RegistrySnapshot, Service } from "@/types/registry"

interface RegistryContextValue {
  snapshot: RegistrySnapshot | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
  saveService: (value: ServiceInput, id?: string) => Promise<Service | null>
  removeService: (id: string) => Promise<boolean>
  saveInstance: (serviceId: string, value: InstanceInput, id?: string) => Promise<boolean>
  setInstanceState: (serviceId: string, id: string, action: "drain" | "enabled", enabled?: boolean) => Promise<boolean>
  removeInstance: (serviceId: string, id: string) => Promise<boolean>
}

const RegistryContext = createContext<RegistryContextValue | null>(null)

export function RegistryProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const [snapshot, setSnapshot] = useState<RegistrySnapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const message = useCallback((cause: unknown) => {
    const code = cause instanceof ApiError ? cause.code : "unknown"
    const key = "errors." + code
    const translated = t(key)
    return translated === key ? t("errors.unknown") : translated
  }, [t])

  const refresh = useCallback(async () => {
    try {
      const next = await api.snapshot()
      setSnapshot(next)
      setError(null)
    } catch (cause) {
      setError(message(cause))
    } finally {
      setLoading(false)
    }
  }, [message])

  useEffect(() => {
    let cancelled = false
    void api.snapshot()
      .then((next) => {
        if (!cancelled) { setSnapshot(next); setError(null) }
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(message(cause))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [message])

  useEffect(() => {
    const timer = window.setInterval(() => { void refresh() }, 5000)
    return () => window.clearInterval(timer)
  }, [refresh])

  const saveService = async (value: ServiceInput, id?: string) => {
    try {
      const saved = await api.saveService(value, id)
      await refresh()
      toast.success(t("common.saved"))
      return saved
    } catch (cause) {
      toast.error(message(cause))
      return null
    }
  }

  const removeService = async (id: string) => {
    try {
      await api.removeService(id)
      await refresh()
      toast.success(t("common.deleted"))
      return true
    } catch (cause) {
      toast.error(message(cause))
      return false
    }
  }

  const saveInstance = async (serviceId: string, value: InstanceInput, id?: string) => {
    try {
      await api.saveInstance(serviceId, value, id)
      await refresh()
      toast.success(t("common.saved"))
      return true
    } catch (cause) {
      toast.error(message(cause))
      return false
    }
  }

  const removeInstance = async (serviceId: string, id: string) => {
    try {
      await api.removeInstance(serviceId, id)
      await refresh()
      toast.success(t("common.deleted"))
      return true
    } catch (cause) {
      toast.error(message(cause))
      return false
    }
  }

  const setInstanceState = async (serviceId: string, id: string, action: "drain" | "enabled", enabled?: boolean) => {
    try {
      await api.setInstanceState(serviceId, id, action, enabled)
      await refresh()
      toast.success(t("common.saved"))
      return true
    } catch (cause) { toast.error(message(cause)); return false }
  }

  return (
    <RegistryContext.Provider value={{ snapshot, loading, error, refresh, saveService, removeService, saveInstance, removeInstance, setInstanceState }}>
      {children}
    </RegistryContext.Provider>
  )
}

export function useRegistry() {
  const context = useContext(RegistryContext)
  if (!context) throw new Error("RegistryProvider is required")
  return context
}
