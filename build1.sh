#!/usr/bin/env bash

prod23="feng@192.168.169.23"
prod35="feng@192.168.169.35"
prod37="feng@192.168.169.37"
basepath=$(
	cd $(dirname $0)
	pwd
)

prod23SlaverDeploy() {
	echo "uploading slaver... ${prod23}"
	rsync ${basepath}/sea_log_slaver ${prod23}:/www/sea_log/slaver/cmd/sea_log_slaver
	rsync ${basepath}/slaver/conf/conf.ini ${prod23}:/www/sea_log/slaver/conf/conf.ini
	ssh ${prod23} <<ssh
    cd /www/sea_log/slaver/cmd
    pm2 reload sea_log_slaver
    exit
ssh
}

prod35SlaverDeploy() {
	echo "uploading slaver... ${prod35}"
	rsync ${basepath}/sea_log_slaver ${prod35}:/www/sea_log/slaver/cmd/sea_log_slaver
	rsync ${basepath}/slaver/conf/conf.ini ${prod35}:/www/sea_log/slaver/conf/conf.ini
	ssh ${prod35} <<ssh
    cd /www/sea_log/slaver/cmd
    pm2 reload sea_log_slaver
    exit
ssh
}

prod37SlaverDeploy() {
	echo "uploading slaver... ${prod37}"
	rsync ${basepath}/sea_log_slaver ${prod37}:/www/sea_log/slaver/cmd/sea_log_slaver
	rsync ${basepath}/slaver/conf/conf.ini ${prod37}:/www/sea_log/slaver/conf/conf.ini
	ssh ${prod37} <<ssh
    cd /www/sea_log/slaver/cmd
    pm2 reload sea_log_slaver
    exit
ssh
}

prod23MasterDeploy() {
	echo "uploading master... ${prod23}"
	rsync ${basepath}/sea_log_master ${prod23}:/www/sea_log/master/cmd/sea_log_master
	rsync ${basepath}/master/conf/conf.ini ${prod23}:/www/sea_log/master/conf/conf.ini
	ssh ${prod23} <<ssh
    cd /www/sea_log/master/cmd
    pm2 reload sea_log_master
    exit
ssh
}

echo "compiling..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sea_log_slaver slaver/cmd/main.go
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sea_log_master master/cmd/main.go

if [ $1 == "prod_slaver" ]; then
	prod23SlaverDeploy
	prod35SlaverDeploy
	prod37SlaverDeploy
elif [ $1 == "prod_master" ]; then
	prod23MasterDeploy
else
	echo "missing deployment parameters..."
fi
