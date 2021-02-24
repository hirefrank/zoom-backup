deploy: 
	./scripts/deploy.sh

destroy: 
	gcloud functions delete backup-zoom-meetings-${NAME} \
		--region=us-central1 \
		--quiet