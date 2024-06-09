package controllers

//go:generate mockery --name=PorterClient --filename=grpc_mocks.go --outpkg=mocks --output=../mocks/grpc
//go:generate mockery --name=ClientConn --filename=clientconn_mocks.go --outpkg=mocks --output=../mocks/grpc
