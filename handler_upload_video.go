package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	//Maximum video upload size is 1GB
	maxFileSize := int64(1 << 30)
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize)

	//Parse video and authenticate user/user authoization
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Parse Video ID from Request.", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Failed to Get Bearer Token To Upload Video.", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Failed to Authenticate User to Upload Video.", err)
		return
	}
	log.Print("User Authenticated")

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Failed To Fetch Video.", err)
		return
	}

	if userID != dbVideo.UserID {
		respondWithError(w, http.StatusUnauthorized, "Cannot Edit Video You Don't Own.", err)
		return
	}
	log.Print("User Authroized to Edit Video")

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Parse Request To Upload Video.", err)
		return
	}
	defer file.Close()
	log.Print("Request Parsed")

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if mediaType != "video/mp4" || err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error Parsing Content-Type to Upload Video.", err)
		return
	} 
	log.Print("Media Type Determined")

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Create Temp Video Upload File.", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	log.Print("tempFile created")

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Copy Video Upload to Temp File.", err)
		return
	}
	log.Print("File Copied Successfully")

	//Generate AWS prefix for video based on Aspect Ratio
	prefix, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Get Video Prefix.", err)
		return
	}

	//Make a random 32 byte base64 filename for the key
	bytes := make([]byte, 32)
	_, err = rand.Read(bytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Fill Bytes for File Name Encoding.", err)
		return
	}

	magicFileName := base64.RawURLEncoding.EncodeToString(bytes)
	key := fmt.Sprintf("%s/%s.mp4",prefix, magicFileName)
	log.Printf("%s Set as Key", key)

	//Process Video for Fast Start
	processedTempFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Process Video for Fast Start.", err)
		return
	}

	processedTempFile, err := os.Open(processedTempFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Open Processed Video.", err)
	}
	defer os.Remove(processedTempFilePath)
	defer processedTempFile.Close()

	//Insert the processed video into s3
	log.Print("Video Starting the be uploaded to s3")
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket: &cfg.s3Bucket,
		Key: &key,
		Body: processedTempFile,
		ContentType: &mediaType,
	})
	log.Print("Video Successfully uploaded to s3")

	//Update the video url in the database 
	videoURL :=  fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
	log.Printf("Video Uploaded to URL: %s\n", videoURL)
	dbVideo.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to Upload Video to DB.", err)
		return
	}
}

func getVideoAspectRatio(filepath string) (string, error) {
	var b bytes.Buffer
	var videoAspectRatio string

	ffprobe := exec.Command("ffprobe", 
		"-v",
		"error",
		"-print_format",
		"json",
		"-show_streams",
		filepath,
	)

	ffprobe.Stdout = &b

	err := ffprobe.Run()
	if err != nil {
		return "", err
	}

	type res struct {
		Streams []struct {
			Width int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	result := res{}
	err = json.Unmarshal(b.Bytes(), &result)
	if err != nil {
		return "", err
	}
	
	width := result.Streams[0].Width
	height := result.Streams[0].Height
	fmt.Printf("Video Width: %d, Video Height: %d\n", width, height)  

	aspectRatio := float64(width) / float64(height)
	switch {
	case math.Abs(aspectRatio - 1.777) < 0.1:
		videoAspectRatio = "landscape"
		log.Printf("Assigned Aspect Ratio: %s\n", videoAspectRatio)
	case math.Abs(aspectRatio - 0.5625) < 0.1:
		videoAspectRatio = "portrait"
		log.Printf("Assigned Aspect Ratio: %s\n", videoAspectRatio)
	default:
		videoAspectRatio = "other"
		log.Printf("Assigned Aspect Ratio: %s\n", videoAspectRatio)
	}

	return videoAspectRatio, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"

	fastStartEncoder := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	err := fastStartEncoder.Run()
	if err != nil {
		return "", err
	}
	log.Printf("%s Processed for Fast Start.\n", outputFilePath)

	return outputFilePath, nil

}
