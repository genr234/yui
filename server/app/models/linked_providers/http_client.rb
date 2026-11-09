require "ipaddr"
require "net/http"
require "resolv"

module LinkedProviders
  # The only way linked-account code talks to the network. It enforces the SSRF rules
  # (plan 16, L6): https only (http only for a private-network host when the admin said
  # the service is on their local network), loopback / link-local / metadata addresses
  # always refused, the name is resolved once and that address is the one connected to,
  # redirects never leave the original host, and every request has timeouts.
  class HttpClient
    CONNECT_TIMEOUT = 5
    READ_TIMEOUT = 30
    MAX_REDIRECTS = 3
    MAX_BODY_BYTES = 16.megabytes # buffered (non-streamed) responses

    Request = Data.define(:method, :uri, :headers, :body)

    # Status, lower-cased headers and a chunk reader. `each_chunk` may be called once.
    class Response
      attr_reader :status, :headers

      def initialize(status, headers, &reader)
        @status = status
        @headers = headers.transform_keys { |key| key.to_s.downcase }
        @reader = reader
      end

      def each_chunk(&block)
        @reader&.call(&block)
      end

      def success?
        status.between?(200, 299)
      end

      def body(limit: MAX_BODY_BYTES)
        buffer = "".b
        each_chunk do |chunk|
          buffer << chunk
          raise ConnectionError, "The response was too large" if buffer.bytesize > limit
        end
        buffer
      end
    end

    # Real network transport. Replaced in tests by a fake service.
    class NetTransport
      def call(request, address)
        uri = request.uri
        http = Net::HTTP.new(uri.host, uri.port)
        http.ipaddr = address
        http.use_ssl = uri.scheme == "https"
        http.open_timeout = CONNECT_TIMEOUT
        http.read_timeout = READ_TIMEOUT
        http.write_timeout = READ_TIMEOUT
        http.ssl_timeout = CONNECT_TIMEOUT
        net_request = Net::HTTPGenericRequest.new(request.method.to_s.upcase, !request.body.nil?, true, uri.request_uri)
        request.headers.each { |name, value| net_request[name] = value }
        net_request["Host"] = uri.port == uri.default_port ? uri.host : "#{uri.host}:#{uri.port}"
        net_request.body = request.body if request.body
        http.start do
          http.request(net_request) do |raw|
            yield Response.new(raw.code.to_i, raw.each_header.to_h) { |&block| raw.read_body(&block) }
          end
        end
      end
    end

    cattr_accessor :transport, default: NetTransport.new
    # Callable(host) -> [ip strings]. Replaced in tests so nothing touches DNS.
    cattr_accessor :resolver, default: ->(host) { Addrinfo.getaddrinfo(host, nil, nil, :STREAM).map(&:ip_address).uniq }

    ALWAYS_REFUSED = [
      "127.0.0.0/8", "0.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4", "100.100.100.200/32",
      "::1/128", "::/128", "fe80::/10", "ff00::/8", "fd00:ec2::254/128"
    ].map { |range| IPAddr.new(range) }.freeze
    PRIVATE_RANGES = [
      "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"
    ].map { |range| IPAddr.new(range) }.freeze

    attr_reader :base_uri

    def initialize(base_url, allow_local_network: false)
      @base_uri = URI.parse(base_url.to_s)
      @allow_local_network = allow_local_network
    end

    # Syntax-only check used when saving; returns an error message or nil.
    def self.url_problem(base_url, allow_local_network: false)
      uri = URI.parse(base_url.to_s)
      return "must start with https://" unless uri.is_a?(URI::HTTP) && uri.host.present?
      return "can't contain a username or password" if uri.userinfo
      return "must start with https://, or http:// on your local network" if uri.scheme == "http" && !allow_local_network

      nil
    rescue URI::InvalidURIError
      "isn't a valid address"
    end

    # Joins `path` (and query) onto the base URL's path.
    def uri_for(path, query = {})
      uri = base_uri.dup
      uri.path = base_uri.path.chomp("/") + path
      uri.query = URI.encode_www_form(query.compact) if query.compact.any?
      uri
    end

    # Yields the Response (streamed; the connection closes after the block) and returns
    # the block's value. Without a block, returns the response with its body buffered.
    def request(method, path, query: {}, headers: {}, form: nil, &block)
      send_request(method, uri_for(path, query), headers: headers, form: form, redirects: 0, &block)
    end

    # Resolves the host once and raises unless every address is allowed.
    def checked_addresses(uri)
      scheme_allowed!(uri)
      host = uri.host.delete_prefix("[").delete_suffix("]")
      raw = begin
        IPAddr.new(host)
        [ host ]
      rescue IPAddr::InvalidAddressError
        self.class.resolver.call(host)
      end
      addresses = Array(raw).map { |value| unwrap(IPAddr.new(value)) }
      raise ConnectionError, "Couldn't reach #{base_uri.host}" if addresses.empty?

      addresses.each { |address| address_allowed!(address, uri) }
      addresses
    rescue SocketError, Resolv::ResolvError, IPAddr::InvalidAddressError
      raise ConnectionError, "Couldn't reach #{base_uri.host}"
    end

    private

    def send_request(method, uri, headers:, form:, redirects:, &block)
      address = checked_addresses(uri).first
      body = form && URI.encode_www_form(form)
      headers = headers.merge("Content-Type" => "application/x-www-form-urlencoded") if form
      request = Request.new(method: method, uri: uri, headers: { "Accept" => "*/*" }.merge(headers), body: body)

      self.class.transport.call(request, address.to_s) do |response|
        if [ 301, 302, 303, 307, 308 ].include?(response.status)
          target = redirect_target(uri, response, redirects)
          return send_request(method, target, headers: headers, form: form, redirects: redirects + 1, &block)
        end
        return block ? yield(response) : buffered(response)
      end
    rescue Timeout::Error, SocketError, SystemCallError, OpenSSL::SSL::SSLError, IOError, Net::ProtocolError => error
      Rails.logger.info("[LinkedProviders::HttpClient] #{uri.host}: #{error.class}")
      raise ConnectionError, "Couldn't reach #{base_uri.scheme}://#{base_uri.host}"
    end

    def buffered(response)
      data = response.body
      Response.new(response.status, response.headers) { |&block| block.call(data) }
    end

    def redirect_target(uri, response, redirects)
      raise ConnectionError, "Too many redirects from #{base_uri.host}" if redirects >= MAX_REDIRECTS

      target = uri.merge(response.headers["location"].to_s)
      unless [ target.scheme, target.host, target.port ] == [ base_uri.scheme, base_uri.host, base_uri.port ]
        raise UnsafeAddressError, "#{base_uri.host} redirected to another address, which Yui won't follow"
      end
      target
    rescue URI::InvalidURIError
      raise ConnectionError, "#{base_uri.host} sent a bad redirect"
    end

    def scheme_allowed!(uri)
      return if uri.scheme == "https"
      return if uri.scheme == "http" && @allow_local_network

      raise UnsafeAddressError, "Use an https:// address (http:// is only allowed on your local network)"
    end

    def unwrap(address)
      address.ipv6? && address.ipv4_mapped? ? address.native : address
    end

    def address_allowed!(address, uri)
      if ALWAYS_REFUSED.any? { |range| range.include?(address) }
        raise UnsafeAddressError, "#{uri.host} points at this server or a reserved address, which isn't allowed"
      end

      private_address = PRIVATE_RANGES.any? { |range| range.include?(address) }
      if private_address && !@allow_local_network
        raise UnsafeAddressError, "#{uri.host} is on a private network. Tick \"My RomM is on my local network\" if that's intended"
      end
      raise UnsafeAddressError, "http:// is only allowed for private-network addresses" if uri.scheme == "http" && !private_address
    end
  end
end
