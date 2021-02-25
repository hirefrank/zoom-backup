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

if [ "$PROJECT_ID" == "" ]
then
    >&2 echo "ERROR: PROJECT_ID is not defined"
    exit 1
fi

if [ "$ZOOM_API_KEY" == "" ]
then
    >&2 echo "ERROR: ZOOM_API_KEY is not defined"
    exit 1
fi

if [ "$ZOOM_API_SECRET" == "" ]
then
    >&2 echo "ERROR: ZOOM_API_SECRET is not defined"
    exit 1
fi

if [ "$ZOOM_USER_ID" == "" ]
then
    >&2 echo "ERROR: ZOOM_USER_ID is not defined"
    exit 1
fi

if [ "$GSTORAGE_BUCKET" == "" ]
then
    >&2 echo "ERROR: GSTORAGE_BUCKET is not defined"
    exit 1
fi
if [ "$ZOOM_TOPIC" == "" ]
then
   ZOOM_TOPIC=$NAME-zoom-backup
fi


# Deployment for zoom backup.
gcloud functions deploy backup-zoom-meetings-$NAME \
    --project=$PROJECT_ID \
    --timeout=540s \
    --entry-point ZoomBackup \
    --region us-central1 \
    --runtime go113 \
    --trigger-topic $ZOOM_TOPIC \
    --service-account $SERVICE_ACCOUNT \
    --set-env-vars [PROJECT_ID=$PROJECT_ID, \
    ZOOM_API_KEY=$ZOOM_API_KEY, \
    ZOOM_API_SECRET=$ZOOM_API_SECRET, \
    ZOOM_USER_ID=$ZOOM_USER_ID, \
    GSTORAGE_BUCKET=$GSTORAGE_BUCKET] \

