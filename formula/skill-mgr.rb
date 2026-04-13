class SkillMgr < Formula
  desc "Agent skill manager — list, audit, and uninstall AI coding agent skills"
  homepage "https://github.com/idrewlong/skill-mgr"
  version "1.0.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_darwin_arm64.tar.gz"
      sha256 "PLACEHOLDER_ARM64_SHA256"
    end
    on_intel do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_darwin_amd64.tar.gz"
      sha256 "PLACEHOLDER_AMD64_SHA256"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_linux_arm64.tar.gz"
      sha256 "PLACEHOLDER_LINUX_ARM64_SHA256"
    end
    on_intel do
      url "https://github.com/idrewlong/skill-mgr/releases/download/v#{version}/skill-mgr_linux_amd64.tar.gz"
      sha256 "PLACEHOLDER_LINUX_AMD64_SHA256"
    end
  end

  def install
    bin.install "skill-mgr"
  end

  test do
    system "#{bin}/skill-mgr", "version"
  end
end
