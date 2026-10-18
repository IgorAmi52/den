class Den < Formula
  desc "Organize AI coding agent sessions into folders with context, beside them in tmux"
  homepage "https://github.com/IgorAmi52/den"
  url "https://github.com/IgorAmi52/den/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "42a5217e14636d4072e5727744ead648387470808c51324a5cbdba2e15b59264"
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
