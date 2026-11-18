package io.github.fkzhao.lightwightns;

import java.util.Map;

public record ResolveQuery(String service, String namespace, String group, String version, Map<String, String> metadata, boolean allowStale) {
    public ResolveQuery(String service) { this(service, "default", "default", "", Map.of(), false); }
    public ResolveQuery { metadata = metadata == null ? Map.of() : Map.copyOf(metadata); }
}
