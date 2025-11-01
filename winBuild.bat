REM Build the Docker/Podman image
podman build -t kansho-build -f Dockerfile.windows .

REM Create a temporary container (does NOT auto-remove)
podman create --name temp-kansho kansho-build

REM Copy the built Windows executable from the container to the current folder
podman cp temp-kansho:/output/kansho.exe "%CD%\kansho.exe"

REM Remove the temporary container
podman rm temp-kansho

