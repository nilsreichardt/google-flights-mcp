REGION=${REGION:-"europe-west1"}
PROJECT_ID=${PROJECT_ID:-"your-project-id"}
# Need to be exported for sudo to work
export IMAGE="gcr.io/$PROJECT_ID/google-flights-cheapest-offers:latest"

set -e # Exit on error

sudo docker buildx build --platform linux/amd64 -t $IMAGE --push .
sudo docker push $IMAGE

gcloud run deploy google-flights-cheapest-offers \
    --image $IMAGE \
    --region $REGION \
    --platform managed \
    --allow-unauthenticated \
    --port 8080 \
    --project $PROJECT_ID
