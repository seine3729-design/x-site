
#!/bin/bash

APK="$1"

if [ -z "$APK" ] || [ ! -f "$APK" ]; then
  echo "用法: $0 app.apk"
  exit 1
fi

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

unzip -Z1 "$APK" > "$TMP" || exit 1

echo "APK: $APK"
echo "========== 技术栈检测 =========="

check() {
  if grep -Eiq "$1" "$TMP"; then
    echo "[命中] $2"
  fi
}

check 'libflutter\.so|assets/flutter_assets/' 'Flutter'
check 'libreactnative\.so|libreactnativejni\.so|index\.android\.bundle' 'React Native 相关特征'
check 'libunity\.so|libil2cpp\.so|assets/bin/Data/' 'Unity'
check 'libmonodroid\.so|assemblies/.*\.dll' '.NET / Xamarin 相关特征'
check 'assets/www/|cordova\.js' 'Cordova / WebView 混合应用'
check 'libhermes\.so' 'Hermes JavaScript 引擎（常见于 React Native）'
check 'classes[0-9]*\.dex' '包含 Android DEX 字节码（不能单独证明是原生开发）'

echo "========== 检测完成 =========="
