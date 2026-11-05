import 'dart:convert';
import 'dart:io';
import 'package:flutter/services.dart';

class CoreException implements Exception {
  final String message;
  CoreException(this.message);
  @override
  String toString() => message;
}

class Backend {
  static const channel = MethodChannel('marshrut/core');
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    final request = jsonEncode({'action': action, ...args});
    String response;
    if (Platform.isAndroid) {
      try {
        response = await channel.invokeMethod<String>('request', request) ?? '';
      } on PlatformException catch (e) {
        throw CoreException(e.message ?? 'Сетевое ядро Android недоступно');
      }
    } else if (Platform.isWindows) {
      final directory = File(Platform.resolvedExecutable).parent.path;
      final executable = '$directory${Platform.pathSeparator}marshrut-core.exe';
      if (!File(executable).existsSync()) {
        throw CoreException(
            'Сетевое ядро не установлено. Запустите установщик Маршрут.');
      }
      final process = await Process.start(executable, ['-mode', 'rpc']);
      // Credentials travel over stdin, never process command-line arguments.
      process.stdin.write(request);
      await process.stdin.close();
      final output = process.stdout.transform(utf8.decoder).join();
      final drain = process.stderr.drain<void>();
      response =
          await output.timeout(const Duration(minutes: 5), onTimeout: () {
        process.kill();
        throw CoreException('Служба не ответила вовремя');
      });
      await drain;
      await process.exitCode;
    } else {
      throw CoreException('Поддерживаются Windows и Android');
    }
    Map<String, dynamic> decoded;
    try {
      decoded = jsonDecode(response) as Map<String, dynamic>;
    } catch (_) {
      throw CoreException('Некорректный ответ сетевого ядра');
    }
    if (decoded['ok'] != true) {
      throw CoreException(decoded['error'] as String? ?? 'Ошибка подключения');
    }
    return decoded['data'] is Map
        ? Map<String, dynamic>.from(decoded['data'] as Map)
        : {};
  }
}
