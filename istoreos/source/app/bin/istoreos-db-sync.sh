#!/bin/sh
# iStoreOS AppCenter 延迟回写守护
# 背景：install_callback 结束后 AppCenter 还会终写一次应用数据行，
# 把回调期间写的 service_url/status/is_stop 冲掉。本脚本在安装后
# 后台运行，周期性校验并在值被冲掉时重写，直到稳定。
APP_NAME="com.istoreos.vm"
WEB_PORT="${ISTO_WEB_PORT:-36125}"
PSQL="psql -h /var/run/postgresql -d appcenter -U postgres"
EXPECTED="http://\${host}:${WEB_PORT}/"

do_sync() {
    $PSQL -c "UPDATE app SET service_url='http://' || chr(36) || '{host}:${WEB_PORT}/', status='running', is_stop=true, is_uninstall=true, updated_at=now() WHERE app_name='${APP_NAME}'" >/dev/null 2>&1
    $PSQL -v ON_ERROR_STOP=0 >/dev/null 2>&1 <<SQL
UPDATE app_service
   SET url='http://' || chr(36) || '{host}:${WEB_PORT}/',
       default_url='http://' || chr(36) || '{host}:${WEB_PORT}/',
       title='iStoreOS',
       icon='ui/images/icon_{0}.png',
       updated_at=now()
 WHERE app_id=(SELECT id FROM app WHERE app_name='${APP_NAME}' LIMIT 1)
   AND service_name='com.istoreos.vm.web';
INSERT INTO app_service(app_id,service_name,title,"desc",icon,type,url,default_url,is_admin,control,no_display,file_types,created_at,updated_at)
SELECT a.id,'com.istoreos.vm.web','iStoreOS','iStoreOS 管理界面 (VM:80)','ui/images/icon_{0}.png','url',
 'http://' || chr(36) || '{host}:${WEB_PORT}/','http://' || chr(36) || '{host}:${WEB_PORT}/',false,
 '{"show":0,"showRoute":0,"auth":0,"port":1,"path":0,"fullUrl":0,"accessPerm":"editable","portPerm":"readonly","pathPerm":"editable","fullUrlPerm":"editable"}',
 false,'[]',now(),now()
FROM app a WHERE a.app_name='${APP_NAME}'
  AND NOT EXISTS (SELECT 1 FROM app_service s WHERE s.app_id=a.id AND s.service_name='com.istoreos.vm.web');
INSERT INTO system_config(type,k,v)
SELECT 'appAutoUpdate','${APP_NAME}','false'
 WHERE NOT EXISTS (SELECT 1 FROM system_config WHERE type='appAutoUpdate' AND k='${APP_NAME}');
SQL
}

i=0
while [ "$i" -lt 40 ]; do
    i=$((i + 1))
    sleep 10
    cnt=$($PSQL -tAc "SELECT count(*) FROM app WHERE app_name='${APP_NAME}'" 2>/dev/null)
    [ "$cnt" != "1" ] && continue
    row=$($PSQL -tAc "SELECT service_url FROM app WHERE app_name='${APP_NAME}'" 2>/dev/null)
    svc=$($PSQL -tAc "SELECT count(*) FROM app_service s JOIN app a ON a.id=s.app_id WHERE a.app_name='${APP_NAME}' AND s.service_name='com.istoreos.vm.web'" 2>/dev/null)
    if [ "$row" != "$EXPECTED" ] || [ "$svc" != "1" ]; then
        do_sync
        echo "round $i: re-synced (app: $row / svc: $svc)"
    fi
done
exit 0
