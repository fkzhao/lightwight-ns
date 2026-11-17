package io.github.fkzhao.lightwightns;

public record AgentStatus(boolean ready, long registryVersion, String lastSyncAt) {}
