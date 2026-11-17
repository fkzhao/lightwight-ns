import { createContext, useCallback, useContext, useEffect, useState } from "react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api, ApiError } from "@/lib/api"
import type { RegistrySnapshot } from "@/types/registry"

type Kind = "services" | "machines" | "instances"

interface RegistryContextValue {
  snapshot: RegistrySnapshot | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
  save: <T>(kind: Kind, value: T, id?: string) => Promise<boolean>
  remove: (kind: Kind, id: string) => Promise<boolean>
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
        if (!cancelled) {
          setSnapshot(next)
          setError(null)
        }
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(message(cause))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [message])

  const save = async <T,>(kind: Kind, value: T, id?: string) => {
    try {
      await api.save(kind, value, id)
      await refresh()
      toast.success(t("common.saved"))
      return true
    } catch (cause) {
      toast.error(message(cause))
      return false
    }
  }

  const remove = async (kind: Kind, id: string) => {
    try {
      await api.remove(kind, id)
      await refresh()
      toast.success(t("common.deleted"))
      return true
    } catch (cause) {
      toast.error(message(cause))
      return false
    }
  }

  return (
    <RegistryContext.Provider value={{ snapshot, loading, error, refresh, save, remove }}>
      {children}
    </RegistryContext.Provider>
  )
}

export function useRegistry() {
  const context = useContext(RegistryContext)
  if (!context) throw new Error("RegistryProvider is required")
  return context
}
