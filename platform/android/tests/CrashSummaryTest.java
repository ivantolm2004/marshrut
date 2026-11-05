import ru.marshrut.client.CrashSummary;

public class CrashSummaryTest {
    public static void main(String[] args) {
        String privateValue = "SECRET_TOKEN_vless://USER_UUID@private.example:443";
        CrashSummary summary = new CrashSummary("panic: runtime error: invalid memory address or nil pointer dereference " + privateValue + "\n"
            + "[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x1234]\n"
            + "runtime.sigpanic()\n"
            + "github.com/sagernet/sing-box/protocol/wireguard.(*Endpoint).Start({" + privateValue + "})\n"
            + "marshrut/mobile.(*Client).Request(0x1234, {" + privateValue + "})\n"
            + "    at ru.marshrut.client.TunnelService.startEngine(TunnelService.kt:100)\n"
            + "    at android.net.Network.bindSocket(Network.java:123)\n"
            + "Caused by: java.lang.NullPointerException: " + privateValue + "\n"
            + "/data/private/" + privateValue + "\n");
        assert summary.category.equals("native_sigsegv");
        assert summary.panicKind.equals("nil_pointer");
        assert summary.signal.equals("SIGSEGV:0x1");
        assert summary.symbols.contains("github.com/sagernet/sing-box/protocol/wireguard.(*Endpoint).Start") : summary.symbols;
        assert summary.symbols.contains("marshrut/mobile.(*Client).Request") : summary.symbols;
        assert summary.symbols.contains("android.net.Network.bindSocket") : summary.symbols;
        assert summary.symbols.size() == 5 : summary.symbols;
        assert summary.exceptionTypes.size() == 1 && summary.exceptionTypes.get(0).equals("java.lang.NullPointerException");
        assert !summary.symbols.toString().contains(privateValue);
        assert !summary.symbols.toString().contains("0x");
        CrashSummary argument = new CrashSummary("runtime.panic({\"runtime.fake(SECRET)\"})\n");
        assert argument.symbols.size() == 1 && argument.symbols.get(0).equals("runtime.panic");
        CrashSummary unknown = new CrashSummary("private.example/user:password\nplain secret\n");
        assert unknown.symbols.isEmpty() && unknown.category.equals("none");
        CrashSummary split = new CrashSummary("fatal error: runtime: stack split at bad time\n");
        assert split.category.equals("go_stack_split");
        System.out.println("Crash summary: symbols, classification and secret redaction passed.");
    }
}
