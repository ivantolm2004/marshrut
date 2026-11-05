#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <windows.h>

#include "flutter_window.h"
#include "utils.h"

int APIENTRY wWinMain(_In_ HINSTANCE instance, _In_opt_ HINSTANCE prev,
                      _In_ wchar_t *command_line, _In_ int show_command) {
  wchar_t module_path[32768];
  DWORD length = ::GetModuleFileNameW(nullptr, module_path, 32768);
  if (length == 0 || length >= 32768) return 2;
  std::wstring directory(module_path, length);
  directory.resize(directory.find_last_of(L"\\/"));
  ::SetCurrentDirectoryW(directory.c_str());
  if (std::wstring(command_line).find(L"--smoke-test") != std::wstring::npos) {
    if (::GetFileAttributesW(L"data\\app.so") == INVALID_FILE_ATTRIBUTES) return 3;
    FlutterDesktopEngineProperties properties = {};
    properties.assets_path = L"data\\flutter_assets";
    properties.icu_data_path = L"data\\icudtl.dat";
    properties.aot_library_path = L"data\\app.so";
    ::CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
    auto engine = FlutterDesktopEngineCreate(&properties);
    if (!engine) return 4;
    FlutterDesktopEngineDestroy(engine);
    ::CoUninitialize();
    return 0;
  }
  // Attach to console when present (e.g., 'flutter run') or create a
  // new console when running with a debugger.
  if (!::AttachConsole(ATTACH_PARENT_PROCESS) && ::IsDebuggerPresent()) {
    CreateAndAttachConsole();
  }

  // Initialize COM, so that it is available for use in the library and/or
  // plugins.
  ::CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);

  flutter::DartProject project(L"data");

  std::vector<std::string> command_line_arguments =
      GetCommandLineArguments();

  project.set_dart_entrypoint_arguments(std::move(command_line_arguments));

  FlutterWindow window(project);
  Win32Window::Point origin(10, 10);
  Win32Window::Size size(1280, 720);
  if (!window.Create(L"Маршрут", origin, size)) {
    return EXIT_FAILURE;
  }
  window.SetQuitOnClose(true);

  ::MSG msg;
  while (::GetMessage(&msg, nullptr, 0, 0)) {
    ::TranslateMessage(&msg);
    ::DispatchMessage(&msg);
  }

  ::CoUninitialize();
  return EXIT_SUCCESS;
}


