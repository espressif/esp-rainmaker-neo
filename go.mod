module github.com/espressif/esp-rainmaker-neo

go 1.26.6

require (
	github.com/aws/aws-lambda-go v1.54.0
	github.com/aws/aws-sdk-go-v2 v1.43.0
	github.com/aws/aws-sdk-go-v2/config v1.32.17
	github.com/aws/aws-sdk-go-v2/credentials v1.19.16 // indirect
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.15.4
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression v1.7.35
	github.com/aws/aws-sdk-go-v2/service/cognitoidentity v1.27.8
	github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider v1.49.1
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.34.10
	github.com/aws/aws-sdk-go-v2/service/ecs v1.57.4
	github.com/aws/aws-sdk-go-v2/service/iot v1.55.4
	github.com/aws/aws-sdk-go-v2/service/iotdataplane v1.24.6
	github.com/aws/aws-sdk-go-v2/service/kinesisvideo v1.33.10
	github.com/aws/aws-sdk-go-v2/service/lambda v1.88.5
	github.com/aws/aws-sdk-go-v2/service/s3 v1.97.3
	github.com/aws/aws-sdk-go-v2/service/sns v1.34.0
	github.com/aws/aws-sdk-go-v2/service/sqs v1.38.5
	github.com/aws/aws-sdk-go-v2/service/ssm v1.56.8
	github.com/aws/aws-sdk-go-v2/service/sts v1.42.1
	github.com/aws/smithy-go v1.27.3
	github.com/go-playground/validator/v10 v10.26.0
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/google/go-cmp v0.7.0
	github.com/google/uuid v1.6.0
	github.com/lestrrat-go/jwx/v2 v2.1.3
	github.com/lithammer/shortuuid/v4 v4.2.0
	github.com/luraim/fun v0.0.0-20240427173526-62ed2a5a9acf
	github.com/onsi/ginkgo/v2 v2.28.3
	github.com/onsi/gomega v1.40.0
	github.com/rs/zerolog v1.34.0
	github.com/wk8/go-ordered-map v1.0.0
	golang.org/x/oauth2 v0.36.0
)

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/Masterminds/semver/v3 v3.5.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.8 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.31 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.31 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.31 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.22.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.9.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.21 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.8 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/goccy/go-json v0.10.3 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lestrrat-go/blackmagic v1.0.2 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.6 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/t-tomalak/logrus-easy-formatter v0.0.0-20190827215021-c074f06c5816 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

require github.com/aws/aws-sdk-go-v2/service/sesv2 v1.62.6

require (
	github.com/aws/aws-sdk-go-v2/service/kms v1.55.0
	github.com/espressif/esp-cloud-common v0.0.0-20260802132405-7e1d4f98e7d6
	golang.org/x/tools v0.48.0
)
