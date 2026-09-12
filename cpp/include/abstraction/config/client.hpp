#pragma once
#include <abstraction/config/rec.h>
#include <abstraction/ipc/frame.hpp>
#include <cstdlib>
#include <optional>
namespace abstraction::config {
inline std::string default_endpoint() {
 if(const char* value=std::getenv("ABSTRACTION_CONFIG_ENDPOINT")){if(*value)return value;}
#ifdef _WIN32
 return R"(\\.\pipe\openabstractions-config-v1)";
#else
 if(const char* value=std::getenv("XDG_RUNTIME_DIR")){if(*value)return std::string(value)+"/openabstractions-config-v1.sock";}
 const char* temp=std::getenv("TMPDIR");const char* user=std::getenv("USER");
 return std::string(temp&&*temp?temp:"/tmp")+"/openabstractions-config-v1-"+(user?user:"")+".sock";
#endif
}
// Read-only capability client. Never reads configuration files or starts a provider.
class Client {
public:
 explicit Client(std::string endpoint=default_endpoint()):endpoint_(std::move(endpoint)){}
 // Explicit operation scope; copies retain the same absolute deadline.
 Client(std::string endpoint, ipc::Deadline deadline):endpoint_(std::move(endpoint)),deadline_(deadline){}
 Snapshot Read()const {
  const auto value=[](const char* name){const char* p=std::getenv(name);return std::string(p?p:"");};
  RunOverrides overrides;
  overrides.nas_store=value("ABSTRACTION_NAS_STORE");overrides.store=value("ABSTRACTION_STORE");
  overrides.log_sink=value("ABSTRACTION_LOG");overrides.log_service=value("ABSTRACTION_LOG_SERVICE");
  return ReadWithOverrides(overrides);
 }
 Snapshot ReadWithOverrides(const RunOverrides& overrides)const {
  auto transport=deadline_?ipc::FrameTransport(endpoint_,*deadline_,1<<20)
                          :ipc::FrameTransport(endpoint_,2000,1<<20);
  ConfigReaderClient<ipc::FrameTransport> client(transport);
  return client.Read(overrides);
 }
private:
 std::string endpoint_;
 std::optional<ipc::Deadline> deadline_;
};
}
