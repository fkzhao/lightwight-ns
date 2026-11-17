import io.github.fkzhao.lightwightns.DiscoveryClient;
import io.github.fkzhao.lightwightns.RegistrationClient;

/** Minimal Java SDK usage with a center and local agent. */
public final class Example {
    public static void main(String[] args) throws Exception {
        if (args.length < 2) throw new IllegalArgumentException("discover SERVICE | register SERVICE IP PORT");
        if (args[0].equals("discover")) {
            String address = System.getenv().getOrDefault("NS_AGENT_TCP_ADDR", "127.0.0.1:9091");
            int separator = address.lastIndexOf(':');
            try (DiscoveryClient agent = new DiscoveryClient(address.substring(0, separator), Integer.parseInt(address.substring(separator + 1)))) {
                System.out.println(agent.resolve(args[1]));
            }
        } else if (args[0].equals("register") && args.length == 4) {
            String centerUrl = System.getenv().getOrDefault("NS_CENTER_HTTP_URL", "http://127.0.0.1:8080");
            RegistrationClient center = new RegistrationClient(centerUrl);
            try (var lease = center.register(args[1], args[2], Integer.parseInt(args[3]))) {
                System.out.println("registered " + lease.instanceId());
                System.in.read();
            }
        } else {
            throw new IllegalArgumentException("discover SERVICE | register SERVICE IP PORT");
        }
    }
}
