#include <abstraction/config/client.hpp>
#include <abstraction/logging/client.hpp>
#include <abstraction/resource/client.hpp>
#include <abstraction/router/client.hpp>
#include <abstraction/facade/bootstrap.hpp>
#include <abstraction/ipc/client.h>
#include <iostream>
#include <vector>
#include <stdexcept>
int main(int argc,char** argv){
 try{
  if(argc==2&&(std::string(argv[1])=="--native"||std::string(argv[1])=="--default-runtime-endpoint")){
   size_t needed=0;auto status=oa_ipc_runtime_endpoint(nullptr,0,&needed);
   if(status!=OA_IPC_OK){if(needed!=0)throw std::runtime_error("failed native size retained");return 23;}
   std::vector<char> buffer(needed);if(oa_ipc_runtime_endpoint(buffer.data(),buffer.size(),&needed)!=OA_IPC_OK)throw std::runtime_error("native endpoint failed");
   std::string native(buffer.data());if(native!=abstraction::facade::bootstrap::runtime_endpoint())throw std::runtime_error("native/facade disagreement");
   std::cout<<native<<"\n";return 0;
  }
  if(argc==3&&std::string(argv[1])=="--namespace"){std::cout<<abstraction::facade::bootstrap::endpoint(argv[2])<<"\n";return 0;}
  const auto actual=abstraction::config::default_endpoint();
  const char* explicit_endpoint=std::getenv("ABSTRACTION_CONFIG_ENDPOINT");
  const auto expected=explicit_endpoint&&*explicit_endpoint?std::string(explicit_endpoint):abstraction::facade::bootstrap::installed_endpoint("config-v1");
  if(actual!=expected)throw std::runtime_error("config default diverges from shared bootstrap");
  if(abstraction::logging::default_endpoint()!=abstraction::facade::bootstrap::installed_endpoint("logging-v1"))throw std::runtime_error("logging default diverges");
  if(abstraction::resource::client::default_endpoint()!=abstraction::facade::bootstrap::installed_endpoint("resource-table-v1"))throw std::runtime_error("resource default diverges");
  if(abstraction::router::default_endpoint()!=abstraction::facade::bootstrap::installed_endpoint("router-v1"))throw std::runtime_error("router default diverges");
  std::cout<<actual<<"\n";return 0;
 }catch(const std::exception&){std::cerr<<"namespace refused\n";return 23;}
}
