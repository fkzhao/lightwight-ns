export type HealthType = "tcp" | "http"
export type HealthStatus = "unknown" | "healthy" | "unhealthy"

export interface HealthCheck {
  type: HealthType
  path: string
}

export interface Instance {
  id: string
  address: string
  port: number
  enabled: boolean
  health: HealthStatus
}

export interface Service {
  id: string
  name: string
  description: string
  healthCheck: HealthCheck
  instances: Instance[]
}

export interface RegistrySnapshot {
  version: number
  services: Service[]
}
