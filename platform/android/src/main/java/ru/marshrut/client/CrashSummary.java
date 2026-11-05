package ru.marshrut.client;

import java.util.ArrayList;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Extract compiled symbol names and fixed classifications, never crash arguments. */
public final class CrashSummary {
    private static final String PREFIX = "(?:(?:runtime|internal|net|crypto|syscall|os|sync|time|marshrut)[./]|github\\.com/(?:sagernet|ViRb3)/|golang\\.org/x/|gvisor\\.dev/gvisor/)";
    private static final Pattern METHOD = Pattern.compile("^(" + PREFIX + "[A-Za-z0-9_./\\[\\]-]*\\.\\(\\*?[A-Za-z0-9_./\\[\\]-]+\\)\\.[A-Za-z0-9_.$\\[\\]-]+)\\(");
    private static final Pattern FUNCTION = Pattern.compile("^(" + PREFIX + "[A-Za-z0-9_./\\[\\]-]*[A-Za-z0-9_\\]])\\(");
    private static final Pattern JAVA_FRAME = Pattern.compile("^\\s*at ((?:ru\\.marshrut\\.|go\\.|java\\.|javax\\.|android\\.|com\\.android\\.|kotlin\\.|io\\.flutter\\.)[A-Za-z0-9_.$]+)\\(");
    private static final Pattern SIGNAL = Pattern.compile("\\[signal (SIGSEGV|SIGABRT|SIGBUS):[^\\r\\n]*?code=(0x[0-9a-fA-F]{1,2}|[0-9]{1,2})(?: |$)");
    private static final Pattern EXCEPTION = Pattern.compile("^\\s*(?:Caused by: )?((?:java\\.|javax\\.|android\\.|go\\.|ru\\.marshrut\\.)[A-Za-z0-9_.$]+(?:Exception|Error))(?::|$)");
    public final String category;
    public final String panicKind;
    public final String signal;
    public final List<String> symbols;
    public final List<String> exceptionTypes;

    public CrashSummary(String text) {
        if (text.length() > 262144) text = text.substring(0, 262144);
        category = text.contains("stack split at bad time") ? "go_stack_split"
            : text.contains("stack overflow") ? "stack_overflow"
            : text.contains("out of memory") ? "out_of_memory"
            : text.contains("SIGSEGV") ? "native_sigsegv"
            : text.contains("SIGABRT") ? "native_sigabrt"
            : text.contains("SIGBUS") ? "native_sigbus"
            : text.contains("panic:") ? "go_panic" : "none";
        panicKind = text.contains("invalid memory address or nil pointer dereference") ? "nil_pointer"
            : text.contains("index out of range") ? "index_out_of_range"
            : text.contains("stack split at bad time") ? "stack_split"
            : text.contains("fatal error: fault") ? "memory_fault"
            : text.contains("concurrent map") ? "concurrent_map"
            : text.contains("panic:") ? "other_panic" : "unknown";
        Matcher fault = SIGNAL.matcher(text);
        signal = fault.find() ? fault.group(1) + ":" + fault.group(2) : "";
        symbols = new ArrayList<>();
        exceptionTypes = new ArrayList<>();
        for (String line : text.split("\\r?\\n")) {
            Matcher exception = EXCEPTION.matcher(line);
            if (exception.find() && exceptionTypes.size() < 8 && !exceptionTypes.contains(exception.group(1))) exceptionTypes.add(exception.group(1));
            if (symbols.size() >= 24) break;
            Matcher match = METHOD.matcher(line);
            if (!match.find()) { match = FUNCTION.matcher(line); if (!match.find()) { match = JAVA_FRAME.matcher(line); if (!match.find()) continue; } }
            String symbol = match.group(1);
            if (symbol.length() <= 200 && !symbols.contains(symbol)) symbols.add(symbol);
        }
    }
}
