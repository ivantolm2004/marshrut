package ru.marshrut.client;

import java.io.IOException;
import java.net.InetSocketAddress;

/** JNI contract: return a real owner, or throw. A null success crashes libbox. */
public final class ConnectionOwnerLookup {
    public interface UidLookup {
        int find(int protocol, InetSocketAddress source, InetSocketAddress destination) throws Exception;
    }
    public interface OwnerFactory<T> {
        T create(int uid) throws Exception;
    }
    public static <T> T resolve(int protocol, InetSocketAddress source,
            InetSocketAddress destination, UidLookup lookup, OwnerFactory<T> factory) throws IOException {
        if (protocol != 6 && protocol != 17) throw new IOException("Неподдерживаемый протокол определения приложения");
        try {
            int uid = lookup.find(protocol, source, destination);
            if (uid < 0) throw new IOException("Приложение для соединения не найдено");
            T owner = factory.create(uid);
            if (owner == null) throw new IOException("Платформа не вернула владельца соединения");
            return owner;
        } catch (IOException failure) {
            throw failure;
        } catch (Exception failure) {
            // System exception messages may include addresses. Keep them out of
            // the Go error; the router can continue without process metadata.
            throw new IOException("Не удалось определить приложение для соединения");
        }
    }
}
