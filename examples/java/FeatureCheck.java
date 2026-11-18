import io.github.fkzhao.lightwightns.*;
import java.net.ServerSocket;
import java.util.Map;

/** Run against the isolated test service created by tests/e2e.py --java. */
public final class FeatureCheck {
    public static void main(String[] args) throws Exception {
        String centerUrl = System.getenv("NS_CENTER_HTTP_URL");
        int agentPort = Integer.parseInt(System.getenv("NS_TEST_AGENT_PORT"));
        try (ServerSocket listener = new ServerSocket(0)) {
            Thread acceptor = new Thread(() -> {
                while (!listener.isClosed()) {
                    try (var connection = listener.accept()) { }
                    catch (Exception error) { return; }
                }
            });
            acceptor.setDaemon(true);
            acceptor.start();
            try (var lease = new RegistrationClient(centerUrl).register("e2e-scoped", "127.0.0.1", listener.getLocalPort(),
                    new RegistrationOptions("prod", "blue", "v2", 25, Map.of("zone", "a")));
                 var agent = new DiscoveryClient("127.0.0.1", agentPort)) {
                ResolveQuery query = new ResolveQuery("e2e-scoped", "prod", "blue", "v2", Map.of("zone", "a"), false);
                long deadline = System.currentTimeMillis() + 10000;
                DiscoveryResult result = agent.resolveV2(query);
                while (result.endpoints().isEmpty() && System.currentTimeMillis() < deadline) {
                    Thread.sleep(100);
                    result = agent.resolveV2(query);
                }
                if (result.endpoints().size() != 1 || result.pickWeighted().weight() != 25) throw new AssertionError("Scoped registration failed");
                lease.drain();
                deadline = System.currentTimeMillis() + 5000;
                while (!agent.resolveV2(query).endpoints().isEmpty() && System.currentTimeMillis() < deadline) Thread.sleep(50);
                if (!agent.resolveV2(query).endpoints().isEmpty()) throw new AssertionError("Drain was not propagated");
            }
        }
        System.out.println("Java scoped registration, weighted discovery, drain, and deregistration: OK");
    }
}
