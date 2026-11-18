package io.github.fkzhao.lightwightns;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.sun.net.httpserver.HttpServer;
import java.io.BufferedReader;
import java.io.BufferedWriter;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class SdkTest {
    @Test void resolvesAndReadsStatus() throws Exception {
        try (ServerSocket server = new ServerSocket(0)) {
            Thread worker = new Thread(() -> {
                try (var socket = server.accept()) {
                    var reader = new BufferedReader(new InputStreamReader(socket.getInputStream(), StandardCharsets.UTF_8));
                    var writer = new BufferedWriter(new OutputStreamWriter(socket.getOutputStream(), StandardCharsets.UTF_8));
                    String line;
                    while ((line = reader.readLine()) != null) {
                        JsonObject request = JsonParser.parseString(line).getAsJsonObject();
                        String result = request.get("method").getAsString().equals("agent.status")
                                ? "{\"ready\":true,\"registryVersion\":7,\"lastSyncAt\":\"now\"}"
                                : "{\"registryVersion\":7,\"endpoints\":[{\"address\":\"127.0.0.1\",\"port\":8081}]}";
                        if (request.get("method").getAsString().equals("discovery.resolveV2")) {
                            result = "{\"registryVersion\":7,\"stale\":false,\"endpoints\":[{\"instanceId\":\"one\",\"address\":\"127.0.0.1\",\"port\":8081,\"weight\":25,\"version\":\"v2\"}]}";
                        }
                        writer.write("{\"jsonrpc\":\"2.0\",\"id\":" + request.get("id") + ",\"result\":" + result + "}\n");
                        writer.flush();
                    }
                } catch (Exception error) { throw new RuntimeException(error); }
            });
            worker.start();
            try (DiscoveryClient client = new DiscoveryClient("127.0.0.1", server.getLocalPort())) {
                assertEquals(List.of(new Endpoint("127.0.0.1", 8081)), client.resolve("orders"));
                assertTrue(client.agentStatus().ready());
                var detailed = client.resolveV2(new ResolveQuery("orders", "prod", "blue", "v2", Map.of(), false));
                assertEquals(25, detailed.pickWeighted().weight());
                assertEquals("v2", detailed.endpoints().get(0).version());
            }
            worker.join(3000);
        }
    }

    @Test void registersAndDeregisters() throws Exception {
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        List<String> methods = new ArrayList<>();
        List<String> tokens = new ArrayList<>();
        server.createContext("/api/registrations", exchange -> {
            JsonObject body = JsonParser.parseString(new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8)).getAsJsonObject();
            methods.add(exchange.getRequestMethod());
            tokens.add(body.get("token").getAsString());
            byte[] response = "{\"instanceId\":\"instance-1\",\"leaseTtlSeconds\":30}".getBytes(StandardCharsets.UTF_8);
            exchange.sendResponseHeaders(200, response.length);
            exchange.getResponseBody().write(response);
            exchange.close();
        });
        server.start();
        try {
            RegistrationClient client = new RegistrationClient("http://127.0.0.1:" + server.getAddress().getPort());
            try (var lease = client.register("orders", "127.0.0.1", 8081, new RegistrationOptions("prod", "blue", "v2", 25, Map.of("zone", "a")))) {
                assertEquals("instance-1", lease.instanceId());
                lease.drain();
            }
            assertEquals(List.of("POST", "POST", "DELETE"), methods);
            assertEquals(tokens.get(0), tokens.get(1));
        } finally { server.stop(0); }
    }
}
