package main

import (
	"bytes"
	"encoding/base64"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/disintegration/imaging"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type RequestConfig struct {
	Bucket    string
	ObjectKey string
	Width     int
	Height    int
}

type JSON map[string]interface{}

// sess 은 모든 aws 에 대한 세션을 제공및 공유
// svc 는 s3에 대한 자격 증명을 위해 구성된 서비스 클라이언트 값 생성
var (
	bucketName = "s3.images.story.io"
	sess       *session.Session
	svc        *s3.S3
)

func init() {
	sess = session.Must(session.NewSession())
	svc = s3.New(sess)
}

// lambda 에서 에러에 대한 response
func errResponse(code int) events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{
		Body: http.StatusText(code),
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
		StatusCode: code,
	}
}

func resizer(req io.Reader, config *RequestConfig) (string, error) {
	// 네트워크 요청으로 받은 파일을 데이터로 읽는다.
	srcImg, err := imaging.Decode(req)
	if err != nil {
		return "", err
	}

	// 현재 리사이징 하려는 이미지의 넓이 / 높이값
	b := srcImg.Bounds()
	// 리사이징 넓이가 현재 이미지 넓이보다 크거나 같으면 원본을 리턴
	if b.Max.X <= config.Width {
		var buf bytes.Buffer
		// 현재 이미지를 JPEG 로만 변경하고 넘겨준다.
		if err := imaging.Encode(&buf, srcImg, imaging.JPEG); err != nil {
			return "", err
		}

		return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
	}

	// Resize srcImage to size = width (px) x height (px) using the Lanczos filter.
	dstImg := imaging.Resize(srcImg, config.Width, config.Height, imaging.Lanczos)

	var buf bytes.Buffer
	// encode 이미지를 지정된 형식 (JPEG, PNG, GIF, TIFF 또는 BMP)으로 변경
	if err := imaging.Encode(&buf, dstImg, imaging.JPEG); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func handler(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	width, _ := strconv.Atoi(req.QueryStringParameters["width"])
	height, _ := strconv.Atoi(req.QueryStringParameters["height"])
	objectKey := req.Path
	// story.io 에 등록된 파일만 허용
	allowedHosts := []string{"images.story.io"}
	if !strings.Contains(strings.Join(allowedHosts, ","), objectKey) {
		return errResponse(http.StatusBadRequest), errors.WithMessage(nil, "NOT_ALLOWED_HOST")
	}

	// 높이 넓이 값이 0보다 작으면 에러
	if width <= 0 && height <= 0 {
		return errResponse(http.StatusBadRequest), errors.WithMessage(nil, "WIDTH AND HEIGHT INVALID DATA")
	}

	config := RequestConfig{
		Width:     width,
		Height:    height,
		Bucket:    bucketName,
		ObjectKey: objectKey,
	}

	// s3에서 데이터를 가져온다
	resp, err := svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(config.Bucket),
		Key:    aws.String(config.ObjectKey),
	})

	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			switch awsErr.Code() {
			case s3.ErrCodeNoSuchBucket:
				fallthrough
			case s3.ErrCodeNoSuchKey:
				return errResponse(http.StatusNotFound), nil
			}
		}

		return errResponse(http.StatusInternalServerError), errors.WithMessage(err, "WIDTH AND HEIGHT INVALID DATA")
	}

	defer resp.Body.Close()

	// 라사이징 데이터
	resize, err := resizer(resp.Body, &config)
	if err != nil {
		return errResponse(http.StatusInternalServerError), errors.WithMessage(err, "RESIZE PARSING ERROR")
	}

	return events.APIGatewayProxyResponse{
		Body: resize,
		Headers: map[string]string{
			"Content-Type":  "image/jpeg",
			"Cache-Control": *resp.CacheControl,
			"Last-Modified": resp.LastModified.Format(http.TimeFormat),
			"ETag":          *resp.ETag,
		},
		StatusCode:      200,
		IsBase64Encoded: true,
	}, nil
}

func main() {
	lambda.Start(handler)
}
