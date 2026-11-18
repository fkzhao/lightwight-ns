package io.github.fkzhao.lightwightns;

import java.util.Map;

public record RegistrationOptions(String namespace, String group, String version, Integer weight, Map<String, String> metadata) {
    public RegistrationOptions() { this("default", "default", "", 100, Map.of()); }
    public RegistrationOptions { metadata = metadata == null ? Map.of() : Map.copyOf(metadata); }
}
