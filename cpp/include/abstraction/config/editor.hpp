#pragma once
#include <abstraction/config/rec.h>
#include <abstraction/ipc/frame.hpp>
#include <optional>

namespace abstraction::config {
// Edits the provider-owned user rung using revision-checked replacement.
// Resolve an editor or deliberately supply its endpoint before constructing it.
class Editor {
public:
 explicit Editor(std::string endpoint):endpoint_(std::move(endpoint)){}
 Editor(std::string endpoint, ipc::Deadline deadline):endpoint_(std::move(endpoint)),deadline_(deadline){}
 UserSnapshot ReadUser() const {
  auto transport=Transport();
  ConfigEditorClient<ipc::FrameTransport> client(transport);
  return client.ReadUser();
 }
 UserReplaceResult ReplaceUser(const std::string& expected_revision, const UserSettings& values) const {
  auto transport=Transport();
  ConfigEditorClient<ipc::FrameTransport> client(transport);
  return client.ReplaceUser(expected_revision,values);
 }
 Editor WithDeadline(ipc::Deadline deadline) const {
  auto scoped=*this;scoped.deadline_=deadline;return scoped;
 }
 Editor WithServerExpectation(std::optional<ipc::ServerExpectation> server) const {auto copy=*this;copy.server_=std::move(server);return copy;}
 Editor WithCancellation(ipc::CancellationToken token) const {
  auto scoped=*this;scoped.cancellation_=std::move(token);return scoped;
 }
private:
 ipc::FrameTransport Transport() const {
  auto transport=deadline_?ipc::FrameTransport(endpoint_,*deadline_,1<<20)
                          :ipc::FrameTransport(endpoint_,2000,1<<20);
  return transport.WithCancellation(cancellation_).WithServerExpectation(server_);
 }
 std::string endpoint_;
 std::optional<ipc::Deadline> deadline_;
 ipc::CancellationToken cancellation_;
 std::optional<ipc::ServerExpectation> server_;
};
}
