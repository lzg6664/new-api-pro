#!/bin/bash
# 导出容器内 PostgreSQL 数据库
# 用法: ./export_db.sh [输出文件名]

CONTAINER_NAME="postgres"
DB_USER="root"
DB_NAME="new-api"
OUTPUT="${1:-backup_$(date +%Y%m%d_%H%M%S).sql}"

echo "正在从容器 $CONTAINER_NAME 导出 $DB_NAME ..."
docker exec -t "$CONTAINER_NAME" pg_dump -U "$DB_USER" "$DB_NAME" > "$OUTPUT"

if [ $? -eq 0 ]; then
    echo "✅ 导出成功: $OUTPUT ($(du -h "$OUTPUT" | cut -f1))"
else
    echo "❌ 导出失败，请检查容器是否在运行"
fi
