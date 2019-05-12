package http3

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/golang/mock/gomock"
	quic "github.com/lucas-clemente/quic-go"
	mockquic "github.com/lucas-clemente/quic-go/internal/mocks/quic"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type mockClient struct {
	closed bool
}

func (m *mockClient) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{Request: req}, nil
}
func (m *mockClient) Close() error {
	m.closed = true
	return nil
}

var _ roundTripCloser = &mockClient{}

type mockBody struct {
	reader   bytes.Reader
	readErr  error
	closeErr error
	closed   bool
}

// make sure the mockBody can be used as a http.Request.Body
var _ io.ReadCloser = &mockBody{}

func (m *mockBody) Read(p []byte) (int, error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	return m.reader.Read(p)
}

func (m *mockBody) SetData(data []byte) {
	m.reader = *bytes.NewReader(data)
}

func (m *mockBody) Close() error {
	m.closed = true
	return m.closeErr
}

var _ = Describe("RoundTripper", func() {
	var (
		rt      *RoundTripper
		req1    *http.Request
		session *mockquic.MockSession
	)

	BeforeEach(func() {
		rt = &RoundTripper{}
		var err error
		req1, err = http.NewRequest("GET", "https://www.example.org/file1.html", nil)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("dialing hosts", func() {
		origDialAddr := dialAddr

		BeforeEach(func() {
			session = mockquic.NewMockSession(mockCtrl)
			origDialAddr = dialAddr
			dialAddr = func(addr string, tlsConf *tls.Config, config *quic.Config) (quic.Session, error) {
				// return an error when trying to open a stream
				// we don't want to test all the dial logic here, just that dialing happens at all
				return session, nil
			}
		})

		AfterEach(func() {
			dialAddr = origDialAddr
		})

		It("creates new clients", func() {
			closed := make(chan struct{})
			testErr := errors.New("test err")
			req, err := http.NewRequest("GET", "https://quic.clemente.io/foobar.html", nil)
			Expect(err).ToNot(HaveOccurred())
			session.EXPECT().OpenUniStreamSync().AnyTimes().Return(nil, testErr)
			session.EXPECT().OpenStreamSync().Return(nil, testErr)
			session.EXPECT().CloseWithError(gomock.Any(), gomock.Any()).Do(func(quic.ErrorCode, error) { close(closed) })
			_, err = rt.RoundTrip(req)
			Expect(err).To(MatchError(testErr))
			Expect(rt.clients).To(HaveLen(1))
			Eventually(closed).Should(BeClosed())
		})

		It("uses the quic.Config, if provided", func() {
			config := &quic.Config{HandshakeTimeout: time.Millisecond}
			var receivedConfig *quic.Config
			dialAddr = func(addr string, tlsConf *tls.Config, config *quic.Config) (quic.Session, error) {
				receivedConfig = config
				return nil, errors.New("handshake error")
			}
			rt.QuicConfig = config
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("handshake error"))
			Expect(receivedConfig.HandshakeTimeout).To(Equal(config.HandshakeTimeout))
		})

		It("uses the custom dialer, if provided", func() {
			var dialed bool
			dialer := func(_, _ string, tlsCfgP *tls.Config, cfg *quic.Config) (quic.Session, error) {
				dialed = true
				return nil, errors.New("handshake error")
			}
			rt.Dial = dialer
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("handshake error"))
			Expect(dialed).To(BeTrue())
		})

		It("reuses existing clients", func() {
			closed := make(chan struct{})
			testErr := errors.New("test err")
			session.EXPECT().OpenUniStreamSync().AnyTimes().Return(nil, testErr)
			session.EXPECT().OpenStreamSync().Return(nil, testErr).Times(2)
			session.EXPECT().CloseWithError(gomock.Any(), gomock.Any()).Do(func(quic.ErrorCode, error) { close(closed) })
			req, err := http.NewRequest("GET", "https://quic.clemente.io/file1.html", nil)
			Expect(err).ToNot(HaveOccurred())
			_, err = rt.RoundTrip(req)
			Expect(err).To(MatchError(testErr))
			Expect(rt.clients).To(HaveLen(1))
			req2, err := http.NewRequest("GET", "https://quic.clemente.io/file2.html", nil)
			Expect(err).ToNot(HaveOccurred())
			_, err = rt.RoundTrip(req2)
			Expect(err).To(MatchError(testErr))
			Expect(rt.clients).To(HaveLen(1))
			Eventually(closed).Should(BeClosed())
		})

		It("doesn't create new clients if RoundTripOpt.OnlyCachedConn is set", func() {
			req, err := http.NewRequest("GET", "https://quic.clemente.io/foobar.html", nil)
			Expect(err).ToNot(HaveOccurred())
			_, err = rt.RoundTripOpt(req, RoundTripOpt{OnlyCachedConn: true})
			Expect(err).To(MatchError(ErrNoCachedConn))
		})
	})

	Context("validating request", func() {
		It("rejects plain HTTP requests", func() {
			req, err := http.NewRequest("GET", "http://www.example.org/", nil)
			req.Body = &mockBody{}
			Expect(err).ToNot(HaveOccurred())
			_, err = rt.RoundTrip(req)
			Expect(err).To(MatchError("http3: unsupported protocol scheme: http"))
			Expect(req.Body.(*mockBody).closed).To(BeTrue())
		})

		It("rejects requests without a URL", func() {
			req1.URL = nil
			req1.Body = &mockBody{}
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("http3: nil Request.URL"))
			Expect(req1.Body.(*mockBody).closed).To(BeTrue())
		})

		It("rejects request without a URL Host", func() {
			req1.URL.Host = ""
			req1.Body = &mockBody{}
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("http3: no Host in request URL"))
			Expect(req1.Body.(*mockBody).closed).To(BeTrue())
		})

		It("doesn't try to close the body if the request doesn't have one", func() {
			req1.URL = nil
			Expect(req1.Body).To(BeNil())
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("http3: nil Request.URL"))
		})

		It("rejects requests without a header", func() {
			req1.Header = nil
			req1.Body = &mockBody{}
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("http3: nil Request.Header"))
			Expect(req1.Body.(*mockBody).closed).To(BeTrue())
		})

		It("rejects requests with invalid header name fields", func() {
			req1.Header.Add("foobär", "value")
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("http3: invalid http header field name \"foobär\""))
		})

		It("rejects requests with invalid header name values", func() {
			req1.Header.Add("foo", string([]byte{0x7}))
			_, err := rt.RoundTrip(req1)
			Expect(err.Error()).To(ContainSubstring("http3: invalid http header field value"))
		})

		It("rejects requests with an invalid request method", func() {
			req1.Method = "foobär"
			req1.Body = &mockBody{}
			_, err := rt.RoundTrip(req1)
			Expect(err).To(MatchError("http3: invalid method \"foobär\""))
			Expect(req1.Body.(*mockBody).closed).To(BeTrue())
		})
	})

	Context("closing", func() {
		It("closes", func() {
			rt.clients = make(map[string]roundTripCloser)
			cl := &mockClient{}
			rt.clients["foo.bar"] = cl
			err := rt.Close()
			Expect(err).ToNot(HaveOccurred())
			Expect(len(rt.clients)).To(BeZero())
			Expect(cl.closed).To(BeTrue())
		})

		It("closes a RoundTripper that has never been used", func() {
			Expect(len(rt.clients)).To(BeZero())
			err := rt.Close()
			Expect(err).ToNot(HaveOccurred())
			Expect(len(rt.clients)).To(BeZero())
		})
	})
})