# Zoom backup cli

This project backs up your Zoom recordings to Google Cloud Storage.

## Usage

Use `.env.example` to create a `.env` file or set the environment variables:

`ZOOM_API_KEY` - Create a JWT app [here](https://marketplace.zoom.us/develop/create) to get your key and secret  
`ZOOM_API_SECRET`  
`ZOOM_USER_ID` - The link to your profile on [this page](https://us02web.zoom.us/account/user#/) contains your User ID (21-ish alphanumeric)  
`GSTORAGE_BUCKET`  
`GSTORAGE_PATH` - Prefix within the bucket  
`GCLOUD_STORAGE_CREDS` - Create a service account with GCS Storage read and
create permissions. Then generate a JSON key for it. The JSON in a `.env` should
be in single quotes and all on one line.  

Then compile and run this code.

`$ go run main.go`

## How it works

1. Generates a JWT from your API key and secret that expires in 35 minutes.
1. Fetches all recordings for the provided user ID.
1. Filters recordings that are not complete or MP4 files.
1. Streams the recording to GCS with the filename containing the start time of
   the recording and recording type. E.g. `2020-09-14T15:02:39Z-shared_screen_with_gallery_views.mp4`
1. Deletes all recordings for the meetings that were not filtered out.

## Contributing

Please open an issue before starting to do work. I don't expect to add many more
features and I wouldn't want you to waste your time on something that doesn't
fit the the project. Otherwise, I'm super happy to have your amazing help! :D

## Author(s)

- [codegoalie](https://codegoalie.com)
