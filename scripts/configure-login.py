#!/usr/bin/env python3
"""Configure OAuth locally; secrets are read without echo and never printed."""
import getpass
import json
import os
from pathlib import Path
import re
import sys
import tempfile


def main():
    if not sys.stdin.isatty():
        raise SystemExit("请在交互式终端运行，避免密钥进入命令参数或日志。")
    base = Path.home() / "Library/Application Support" if sys.platform == "darwin" else Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config"))
    default = base / "Monee"
    data_dir = Path(os.environ.get("MONEE_DATA_DIR", default)).expanduser()
    data_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    path = data_dir / "auth.json"
    config = json.loads(path.read_text()) if path.exists() else {"google": {}, "github": {}, "superadmins": []}
    print("配置 Monee 登录。已有值按回车保留；Client Secret 输入不会显示。")
    for provider in ("github", "google"):
        current = config.get(provider, {})
        client_id = input(f"{provider} Client ID（留空保留或跳过）：").strip() or current.get("clientId", "")
        secret = current.get("clientSecret", "")
        if client_id:
            secret = getpass.getpass(f"{provider} Client Secret（留空保留）：").strip() or secret
            if not secret:
                raise SystemExit(f"{provider} 缺少 Client Secret，配置尚未保存。")
        config[provider] = {"clientId": client_id, "clientSecret": secret}
        if provider == "github":
            default_type = current.get("appType") or ("github-app" if client_id.startswith("Iv") else "oauth-app")
            app_type = input(f"GitHub 应用类型 github-app / oauth-app（默认 {default_type}）：").strip() or default_type
            if app_type not in ("github-app", "oauth-app"):
                raise SystemExit("GitHub 应用类型无效，配置尚未保存。")
            config[provider]["appType"] = app_type
    if not config.get("superadmins"):
        print("首次初始化超管；至少提供一种身份。GitHub 需要数值账号 ID，Google 可用验证邮箱。")
        github_id = input("超管 GitHub 数值 ID（可留空）：").strip()
        google_email = input("超管 Google 邮箱（可留空）：").strip().lower()
        seeds = []
        if github_id:
            if not re.fullmatch(r"[1-9][0-9]*", github_id):
                raise SystemExit("GitHub ID 必须为数值，配置尚未保存。")
            seeds.append({"provider": "github", "kind": "subject", "value": github_id, "note": "初始超管"})
        if google_email:
            if "@" not in google_email:
                raise SystemExit("Google 邮箱格式无效，配置尚未保存。")
            seeds.append({"provider": "google", "kind": "email", "value": google_email, "note": "初始超管"})
        if not seeds:
            raise SystemExit("需要初始超管身份，配置尚未保存。")
        config["superadmins"] = seeds
    descriptor, temporary = tempfile.mkstemp(prefix="auth-", suffix=".tmp", dir=data_dir)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w") as output:
            json.dump(config, output, ensure_ascii=False, indent=2)
            output.write("\n")
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    print("登录配置已保存在本机私有目录。请重启 Monee 本地服务后登录。")


if __name__ == "__main__":
    try:
        main()
    except (KeyboardInterrupt, EOFError):
        raise SystemExit("\n已取消，原配置保持不变。")
