package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// set the upload limit
	const uploadLimit = 1 << 30 // 1GB
	r.Body = http.MaxBytesReader(w, r.Body, int64(uploadLimit))

	// retreive the video id
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid video id", err)
		return
	}

	// authenticate the user
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video with given id", err)
		return
	}
	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "User is not the video owner", err)
		return
	}

	// parse the file uploaded by html form named video
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse the file", err)
		return
	}
	defer file.Close()

	// extracts the media type and check its type
	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse value of content type header", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type, only MP4 is allowed", nil)
		return
	}

	// create a temp file to be read by s3 after copying the original file to it
	// this is done because mulitpart.file is a one-read stream and you can't read it twice or adjust its pointer
	tempfile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
		return
	}
	defer os.Remove(tempfile.Name())
	defer tempfile.Close()

	if _, err = io.Copy(tempfile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy file to tempfile", err)
		return
	}

	_, err = tempfile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't move the tempfile's pointer", err)
		return
	}

	// get video aspect ratio
	aspectRatio, err := getVideoAspectRatio(tempfile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video's aspect ratio", err)
		return
	}

	prefix := ""
	switch aspectRatio {
	case "16:9":
		prefix = "landscape"
	case "9:16":
		prefix = "portrait"
	default:
		prefix = "other"
	}

	// generate a random key for the s3 object
	key := getAssetPath(mediaType)
	key = path.Join(prefix, key)

	// process the tempfile and use the processed version
	processedFilePath, err := processVideoForFastStart(tempfile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't process tempfile", err)
		return
	}
	defer os.Remove(processedFilePath)

	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't open the processed file", err)
		return
	}
	defer processedFile.Close()

	// save the video to s3
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        processedFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload file to s3", err)
		return
	}

	// update the url of the video uploaded to s3
	// url := cfg.getS3ObjectURL(key)
	url := fmt.Sprintf("%s,%s", cfg.s3Bucket, key)
	video.VideoURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't save video metadata to db", err)
		return
	}

	videoWithPresignURL, err := cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate presign url for video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoWithPresignURL)
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	s3Info := strings.Split(*video.VideoURL, ",")
	bucket, key := s3Info[0], s3Info[1]
	urlExpireTime := 1 * time.Hour
	presignURL, err := generatePresignedURL(cfg.s3Client, bucket, key, urlExpireTime)
	if err != nil {
		return video, err
	}
	video.VideoURL = &presignURL
	return video, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s3Client)
	presignRequest, err := presignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return presignRequest.URL, nil
}
