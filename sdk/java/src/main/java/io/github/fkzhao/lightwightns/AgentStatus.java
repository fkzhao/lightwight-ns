package io.github.fkzhao.lightwightns;

public record AgentStatus(boolean ready, long registryVersion, String lastSyncAt, boolean stale) {
    public AgentStatus(boolean ready, long registryVersion, String lastSyncAt) { this(ready, registryVersion, lastSyncAt, false); }
}
