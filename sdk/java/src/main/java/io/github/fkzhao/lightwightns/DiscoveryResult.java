package io.github.fkzhao.lightwightns;

import java.util.List;
import java.util.concurrent.ThreadLocalRandom;

public record DiscoveryResult(long registryVersion, List<Instance> endpoints, boolean stale) {
    public Instance pickWeighted() {
        int total = endpoints.stream().mapToInt(item -> Math.max(0, item.weight())).sum();
        if (total == 0) throw new IllegalStateException("No eligible endpoints");
        int choice = ThreadLocalRandom.current().nextInt(total);
        for (Instance endpoint : endpoints) {
            choice -= Math.max(0, endpoint.weight());
            if (choice < 0) return endpoint;
        }
        throw new IllegalStateException("No eligible endpoints");
    }
}
