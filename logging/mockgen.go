package logging

//go:generate sh -c "mockgen -package logging -destination mock_connection_tracer_test.go github.com/IRelaxxx/quic-go/logging ConnectionTracer"
//go:generate sh -c "mockgen -package logging -destination mock_tracer_test.go github.com/IRelaxxx/quic-go/logging Tracer"
