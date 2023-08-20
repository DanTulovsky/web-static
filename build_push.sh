export IMAGE_NAME="frontend"

export VERSION="0.0.44"

echo "Building local/$IMAGE_NAME"
docker build . --file Dockerfile --tag local/$IMAGE_NAME

echo "Tagging local/$IMAGE_NAME $IMAGE_ID:$VERSION"
docker tag local/$IMAGE_NAME $IMAGE_ID:$VERSION
docker tag local/$IMAGE_NAME $IMAGE_ID:latest

echo "Pushing $IMAGE_ID:$VERSION"
docker push $IMAGE_ID:$VERSION
docker push $IMAGE_ID:latest
