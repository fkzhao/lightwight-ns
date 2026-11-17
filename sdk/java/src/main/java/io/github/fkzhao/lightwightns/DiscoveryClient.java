package io.github.fkzhao.lightwightns;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.BufferedWriter;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

/** Reuses one TCP connection to the machine-local agent. */
public final class DiscoveryClient implements AutoCloseable {
    private final Socket socket;
    private final BufferedReader reader;
    private final BufferedWriter writer;
    private long nextId;

    public DiscoveryClient() throws IOException { this("127.0.0.1", 9091); }

    public DiscoveryClient(String host, int port) throws IOException {
        socket = new Socket();
        socket.connect(new InetSocketAddress(host, port), 3000);
        socket.setSoTimeout(3000);
        reader = new BufferedReader(new InputStreamReader(socket.getInputStream(), StandardCharsets.UTF_8));
        writer = new BufferedWriter(new OutputStreamWriter(socket.getOutputStream(), StandardCharsets.UTF_8));
    }

    private synchronized JsonObject call(String method, JsonObject params) throws IOException {
        long id = ++nextId;
        JsonObject request = new JsonObject();
        request.addProperty("jsonrpc", "2.0");
        request.addProperty("id", id);
        request.addProperty("method", method);
        request.add("params", params);
        writer.write(request.toString());
        writer.newLine();
        writer.flush();
        String line = reader.readLine();
        if (line == null || line.length() > 1_048_576) throw new IOException("Agent response missing or too large");
        JsonObject response = JsonParser.parseString(line).getAsJsonObject();
        if (!"2.0".equals(response.get("jsonrpc").getAsString()) || response.get("id").getAsLong() != id) {
            throw new IOException("Invalid agent RPC response");
        }
        if (response.has("error")) {
            JsonObject error = response.getAsJsonObject("error");
            throw new AgentRpcException(error.get("code").getAsInt(), error.get("message").getAsString());
        }
        return response.getAsJsonObject("result");
    }

    public List<Endpoint> resolve(String serviceName) throws IOException {
        JsonObject params = new JsonObject();
        params.addProperty("service", serviceName);
        JsonArray endpoints = call("discovery.resolve", params).getAsJsonArray("endpoints");
        List<Endpoint> result = new ArrayList<>();
        for (var item : endpoints) {
            JsonObject endpoint = item.getAsJsonObject();
            result.add(new Endpoint(endpoint.get("address").getAsString(), endpoint.get("port").getAsInt()));
        }
        return result;
    }

    public AgentStatus agentStatus() throws IOException {
        JsonObject status = call("agent.status", new JsonObject());
        return new AgentStatus(status.get("ready").getAsBoolean(),
                status.get("registryVersion").getAsLong(), status.get("lastSyncAt").getAsString());
    }

    @Override public void close() throws IOException { socket.close(); }
}
