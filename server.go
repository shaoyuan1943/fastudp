package fastudp

type Server struct {
	network     string
	addr        string
	reusePort   bool
	numListener int
}

func NewUDPServer(network string, addr string, reusePort bool, numListener int) (*Server, error) {
	server := &Server{
		network:     network,
		addr:        addr,
		reusePort:   reusePort,
		numListener: numListener,
	}

	err := server.start()
	if err != nil {
		return nil, err
	}

	return server, nil
}

func (s *Server) start() error {

}
