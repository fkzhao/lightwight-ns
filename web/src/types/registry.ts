export type HealthType = "tcp" | "http" | "grpc"
export type HealthStatus = "unknown" | "healthy" | "unhealthy"

export interface HealthCheck {
  type: HealthType
  path: string
  failureThreshold?: number
  successThreshold?: number
  intervalMillis?: number
  timeoutMillis?: number
  grpcService?: string
  tls?: boolean
  serverName?: string
  caPem?: string
}

export interface ProtectionPolicy {
  enabled: boolean
  allowStale: boolean
  minInstances?: number
  renewalThreshold?: number
  probeFailureThreshold?: number
  windowSeconds?: number
  warmupSeconds?: number
  maxHoldSeconds?: number
  recoverySeconds?: number
  maxStaleSeconds?: number
}

export interface Instance {
  id: string
  address: string
  port: number
  enabled: boolean
  health: HealthStatus
  source?: "sdk"
  leaseExpiresAt?: number
  leaseExpired?: boolean
  lifecycle: "active" | "draining"
  version?: string
  weight?: number
  metadata?: Record<string, string>
  lastError?: string
  lastCheckAt?: number
  lastHealthyAt?: number
  durationMillis?: number
  consecutiveFailures?: number
  consecutiveSuccesses?: number
}

export interface Service {
  id: string
  name: string
  namespace: string
  group: string
  description: string
  healthCheck: HealthCheck
  protection: ProtectionPolicy
  protectionStatus: { active: boolean; reason?: string; renewalRatio: number }
  instances: Instance[]
}

export interface RegistrySnapshot {
  version: number
  services: Service[]
}
