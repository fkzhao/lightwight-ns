package io.github.fkzhao.lightwightns;

import com.google.gson.JsonObject;
import com.google.gson.Gson;
import com.google.gson.JsonParser;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.security.SecureRandom;
import java.time.Duration;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/** Registers an instance with the center and renews its lease automatically. */
public final class RegistrationClient {
    private final String centerUrl;
    private final HttpClient http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(3)).build();

    public RegistrationClient() { this("http://127.0.0.1:8080"); }

    public RegistrationClient(String centerUrl) {
        this.centerUrl = centerUrl.replaceAll("/+$", "");
    }

    private JsonObject request(String method, String path, JsonObject body) throws IOException, InterruptedException {
        HttpRequest request = HttpRequest.newBuilder(URI.create(centerUrl + path))
                .timeout(Duration.ofSeconds(3))
                .header("Content-Type", "application/json")
                .method(method, HttpRequest.BodyPublishers.ofString(body.toString()))
                .build();
        HttpResponse<String> response = http.send(request, HttpResponse.BodyHandlers.ofString());
        if (response.statusCode() < 200 || response.statusCode() >= 300) {
            String code = "unknown_error";
            try { code = JsonParser.parseString(response.body()).getAsJsonObject().get("code").getAsString(); }
            catch (RuntimeException ignored) { }
            throw new RegistrationException(response.statusCode(), code);
        }
        return response.statusCode() == 204 ? new JsonObject() : JsonParser.parseString(response.body()).getAsJsonObject();
    }

    public RegistrationLease register(String serviceName, String address, int port) throws IOException, InterruptedException {
        return register(serviceName,address,port,new RegistrationOptions());
    }

    public RegistrationLease register(String serviceName, String address, int port, RegistrationOptions options) throws IOException, InterruptedException {
        byte[] secret = new byte[32];
        new SecureRandom().nextBytes(secret);
        StringBuilder token = new StringBuilder(64);
        for (byte value : secret) token.append(String.format("%02x", value & 0xff));
        JsonObject body = new Gson().toJsonTree(options).getAsJsonObject();
        body.addProperty("serviceName", serviceName);
        body.addProperty("address", address);
        body.addProperty("port", port);
        body.addProperty("token", token.toString());
        JsonObject result = request("POST", "/api/registrations", body);
        return new RegistrationLease(body, result);
    }

    public final class RegistrationLease implements AutoCloseable {
        private final JsonObject body;
        private final ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor(r -> {
            Thread thread = new Thread(r, "lightwight-ns-renewal");
            thread.setDaemon(true);
            return thread;
        });
        private volatile String instanceId;
        private volatile Exception lastRenewalError;
        private boolean closed;

        private RegistrationLease(JsonObject body, JsonObject result) {
            this.body = body;
            instanceId = result.get("instanceId").getAsString();
            long interval = Math.max(1, result.get("leaseTtlSeconds").getAsLong() / 3);
            scheduler.scheduleWithFixedDelay(this::renew, interval, interval, TimeUnit.SECONDS);
        }

        private void renew() {
            try {
                JsonObject result = request("POST", "/api/registrations", body);
                instanceId = result.get("instanceId").getAsString();
                lastRenewalError = null;
            } catch (Exception error) {
                lastRenewalError = error;
            }
        }

        public String instanceId() { return instanceId; }
        public Exception lastRenewalError() { return lastRenewalError; }

        public void drain() throws IOException, InterruptedException {
            JsonObject token = new JsonObject();
            token.addProperty("token", body.get("token").getAsString());
            request("POST", "/api/registrations/" + instanceId + "/drain", token);
        }

        @Override public synchronized void close() throws IOException, InterruptedException {
            if (closed) return;
            scheduler.shutdown();
            if (!scheduler.awaitTermination(4, TimeUnit.SECONDS)) {
                scheduler.shutdownNow();
            }
            JsonObject token = new JsonObject();
            token.addProperty("token", body.get("token").getAsString());
            try { request("DELETE", "/api/registrations/" + instanceId, token); }
            catch (RegistrationException error) { if (error.status() != 404) throw error; }
            closed = true;
        }
    }
}
