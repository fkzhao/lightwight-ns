package io.github.fkzhao.lightwightns;

import java.util.Map;

public record Instance(String instanceId, String address, int port, String version, int weight, Map<String, String> metadata, boolean stale) {
    public Instance { metadata = metadata == null ? Map.of() : Map.copyOf(metadata); version = version == null ? "" : version; }
}
