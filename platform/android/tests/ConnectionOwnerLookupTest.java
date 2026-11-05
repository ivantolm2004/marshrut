import java.io.IOException;
import java.net.InetSocketAddress;
import ru.marshrut.client.ConnectionOwnerLookup;

public class ConnectionOwnerLookupTest {
    public static void main(String[] args) throws Exception {
        for (int protocol : new int[]{6,17}) {
            InetSocketAddress source = new InetSocketAddress(protocol == 6 ? "192.0.2.1" : "2001:db8::1", 1234);
            InetSocketAddress destination = new InetSocketAddress(protocol == 6 ? "192.0.2.2" : "2001:db8::2", 443);
            String owner = ConnectionOwnerLookup.resolve(protocol, source, destination, (p,s,d) -> {
                assert p == protocol && s == source && d == destination;
                return 12345;
            }, uid -> "uid=" + uid);
            assert owner.equals("uid=12345");
            expectFailure(() -> ConnectionOwnerLookup.resolve(protocol, source, destination,
                (p,s,d) -> -1, uid -> { throw new AssertionError("invalid UID reached factory"); }));
            expectFailure(() -> ConnectionOwnerLookup.resolve(protocol, source, destination,
                (p,s,d) -> { throw new SecurityException("SECRET_SERVER_ADDRESS"); }, uid -> "owner"));
            expectFailure(() -> ConnectionOwnerLookup.resolve(protocol, source, destination,
                (p,s,d) -> 12345, uid -> null));
        }
        expectFailure(() -> ConnectionOwnerLookup.resolve(1, null, null,
            (p,s,d) -> { throw new AssertionError("unsupported protocol reached Android API"); }, uid -> "owner"));
        System.out.println("Owner lookup: TCP/UDP IPv4/IPv6, unknown UID, permission denial and no null-success passed.");
    }
    private interface Attempt { void run() throws Exception; }
    private static void expectFailure(Attempt attempt) throws Exception {
        try { attempt.run(); throw new AssertionError("expected a regular lookup error"); }
        catch (IOException expected) { assert !expected.getMessage().contains("SECRET_SERVER_ADDRESS"); }
    }
}
