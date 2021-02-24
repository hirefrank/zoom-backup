#!/usr/bin/env bash

set -e
set -o errexit

if [ "$SERVICE_ACCOUNT" == "" ]
then
    >&2 echo "ERROR: SERVICE_ACCOUNT is not defined"
    exit 1
fi

if [ "$NAME" == "" ]
then
    >&2 echo "ERROR: NAME is not defined"
    exit 1
fi

if [ "$ZOOM_TOPIC" == "" ]
then
   ZOOM_TOPIC=$NAME-zoom-backup
fi


# Deployment for zoom backup.
gcloud functions deploy backup-zoom-meetings-$NAME \
    --timeout=540s \
    --env-vars-file env.yml \
    --entry-point ZoomBackup \
    --region us-central1 \
    --runtime go113 \
    --trigger-topic $ZOOM_TOPIC \
    --service-account $SERVICE_ACCOUNT

