#!/bin/sh
set -eu

mkdir -p release/uploads
cat > release/config.json <<EOF
{
  "server": {
    "address": "127.0.0.1:9090", "public_url": "http://127.0.0.1:9090", "production": false,
    "read_header_timeout": "5s", "read_timeout": "15s", "write_timeout": "30s", "idle_timeout": "1m", "shutdown_timeout": "15s",
    "trusted_proxies": ["127.0.0.1"]
  },
  "database": {
    "host": "127.0.0.1", "port": 3306, "name": "goweb", "user": "root", "password": "root",
    "parameters": {"charset": "utf8mb4", "collation": "utf8mb4_unicode_ci"},
    "connect_timeout": "10s", "migration_timeout": "2m", "max_open_connections": 10, "max_idle_connections": 5, "connection_max_lifetime": "5m"
  },
  "authentication": {"session_lifetime": "1h", "secure_cookies": false, "login_max_attempts": 3, "login_lockout": "2s"},
  "storage": {"uploads_directory": "uploads", "max_upload_bytes": 10485760, "allowed_mime_types": ["image/jpeg", "image/png", "application/pdf", "text/plain"]},
  "backup": {"retention_count": 2},
  "email": {"enabled": false, "host": "", "port": 587, "username": "", "password": "", "from_name": "GoWeb", "from_address": "", "encryption": "starttls", "timeout": "10s"},
  "service": {"name": "goweb", "description": "GoWeb CI", "user": "", "group": ""}
}
EOF
