class Den < Formula
  desc "Organize AI coding agent sessions into folders with context, beside them in tmux"
  homepage "https://github.com/IgorAmi52/den"
  url "https://github.com/IgorAmi52/den/archive/refs/tags/v0.2.0.tar.gz"
  sha256 "525828132de8b438d99bf59e2ebf8ae47f79eb2ef94f44bbc4d161178dc05b94"
  head "https://github.com/IgorAmi52/den.git", branch: "main"

  depends_on "go" => :build
  depends_on :macos
  depends_on "tmux"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w")
  end

  test do
    assert_predicate bin/"den", :executable?
  end
end
