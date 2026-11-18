import com.sun.net.httpserver.HttpServer;
import io.github.fkzhao.lightwightns.RegistrationClient;
import java.net.InetSocketAddress;
import java.util.concurrent.CountDownLatch;

/** The JDK shutdown hook also works when a process receives SIGTERM. */
public final class GracefulService {
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("NS_SERVICE_IP", "127.0.0.1");
        int port = Integer.parseInt(System.getenv().getOrDefault("NS_SERVICE_PORT", "8081"));
        HttpServer server = HttpServer.create(new InetSocketAddress(address, port), 0);
        server.createContext("/", exchange -> {
            exchange.sendResponseHeaders(exchange.getRequestURI().getPath().equals("/health") ? 204 : 200, -1);
            exchange.close();
        });
        server.start();
        var center = new RegistrationClient(System.getenv().getOrDefault("NS_CENTER_HTTP_URL", "http://127.0.0.1:8080"));
        RegistrationClient.RegistrationLease lease;
        try { lease = center.register("order-service", address, server.getAddress().getPort()); }
        catch (Exception error) { server.stop(0); throw error; }
        CountDownLatch stopped = new CountDownLatch(1);
        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            try { lease.drain(); } catch (Exception error) { System.err.println("Drain failed: " + error); }
            server.stop(10);
            try { lease.close(); } catch (Exception error) { System.err.println("Deregistration failed: " + error); }
            stopped.countDown();
        }));
        stopped.await();
    }
}
