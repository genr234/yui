require "test_helper"

class LinkedProviders::HttpClientTest < ActiveSupport::TestCase
  include FakeRommTestHelper

  setup { install_fake_romm }
  teardown { remove_fake_romm }

  def client(url = "https://romm.example.test", local: false)
    LinkedProviders::HttpClient.new(url, allow_local_network: local)
  end

  def resolve_to(*addresses)
    LinkedProviders::HttpClient.resolver = ->(_host) { addresses }
  end

  test "ssrf table: refused addresses" do
    {
      "loopback name" => "127.0.0.1",
      "loopback v6" => "::1",
      "link-local" => "169.254.10.10",
      "cloud metadata" => "169.254.169.254",
      "link-local v6" => "fe80::1",
      "metadata v6" => "fd00:ec2::254",
      "mapped loopback" => "::ffff:127.0.0.1",
      "unspecified" => "0.0.0.0"
    }.each do |name, address|
      resolve_to(address)
      assert_raises(LinkedProviders::UnsafeAddressError, name) { client.request(:get, "/api/heartbeat") }
      # Not even the local-network tick makes these acceptable.
      assert_raises(LinkedProviders::UnsafeAddressError, "#{name} with local network") do
        client("http://romm.example.test", local: true).request(:get, "/api/heartbeat")
      end
    end
    assert_empty @fake_romm.requests
  end

  test "ssrf table: a name with one bad address among good ones is refused" do
    resolve_to("93.184.216.34", "127.0.0.1")
    assert_raises(LinkedProviders::UnsafeAddressError) { client.request(:get, "/api/heartbeat") }
  end

  test "ssrf table: literal loopback and link-local hosts are refused without DNS" do
    assert_raises(LinkedProviders::UnsafeAddressError) { client("https://127.0.0.1").request(:get, "/api/heartbeat") }
    assert_raises(LinkedProviders::UnsafeAddressError) { client("https://[::1]:8443").request(:get, "/api/heartbeat") }
    assert_raises(LinkedProviders::UnsafeAddressError) { client("https://169.254.169.254").request(:get, "/latest/meta-data") }
  end

  test "ssrf table: http needs the local-network tick and a private address" do
    assert_raises(LinkedProviders::UnsafeAddressError) { client("http://romm.example.test").request(:get, "/api/heartbeat") }

    resolve_to("192.168.1.20")
    assert_raises(LinkedProviders::UnsafeAddressError) { client("http://nas.local").request(:get, "/api/heartbeat") }
    assert_equal 200, client("http://nas.local", local: true).request(:get, "/api/heartbeat").status

    resolve_to("93.184.216.34")
    assert_raises(LinkedProviders::UnsafeAddressError, "http to a public address") do
      client("http://romm.example.test", local: true).request(:get, "/api/heartbeat")
    end
  end

  test "ssrf table: private addresses need the tick even over https" do
    resolve_to("10.0.0.5")
    assert_raises(LinkedProviders::UnsafeAddressError) { client.request(:get, "/api/heartbeat") }
    assert_equal 200, client("https://romm.example.test", local: true).request(:get, "/api/heartbeat").status
  end

  test "ssrf table: a redirect to another host is not followed" do
    @fake_romm.redirect_to = "https://elsewhere.example.test/api/platforms"
    assert_raises(LinkedProviders::UnsafeAddressError) { client.request(:get, "/api/platforms") }
    assert_equal [ "GET /api/platforms" ], @fake_romm.calls
  end

  test "ssrf table: a redirect to the same host is followed and re-checked" do
    @fake_romm.redirect_to = "https://romm.example.test/api/heartbeat"
    assert_equal 200, client.request(:get, "/api/platforms").status
  end

  test "ssrf table: a redirect to a link-local ip is refused" do
    @fake_romm.redirect_to = "https://169.254.169.254/latest/meta-data"
    assert_raises(LinkedProviders::UnsafeAddressError) { client.request(:get, "/api/platforms") }
  end

  test "connects to the address that was checked" do
    client.request(:get, "/api/heartbeat")
    assert_equal FakeRommTestHelper::PUBLIC_ADDRESS, @fake_romm.last_address
  end

  test "dns failure is a connection error" do
    LinkedProviders::HttpClient.resolver = ->(_host) { raise SocketError, "no such host" }
    error = assert_raises(LinkedProviders::ConnectionError) { client.request(:get, "/api/heartbeat") }
    assert_match(/Couldn't reach romm.example.test/, error.message)
  end

  test "url_problem rejects bad shapes" do
    assert_nil LinkedProviders::HttpClient.url_problem("https://romm.example.test")
    assert_match(/https/, LinkedProviders::HttpClient.url_problem("http://romm.example.test"))
    assert_nil LinkedProviders::HttpClient.url_problem("http://192.168.1.2:8080", allow_local_network: true)
    assert_match(/username/, LinkedProviders::HttpClient.url_problem("https://user:pw@romm.example.test"))
    assert_not_nil LinkedProviders::HttpClient.url_problem("ftp://romm.example.test")
    assert_not_nil LinkedProviders::HttpClient.url_problem("not a url")
  end

  test "the real transport has the required timeouts" do
    assert_equal 5, LinkedProviders::HttpClient::CONNECT_TIMEOUT
    assert_equal 30, LinkedProviders::HttpClient::READ_TIMEOUT
  end
end
