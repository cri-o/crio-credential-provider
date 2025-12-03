# Built from this template:
# https://github.com/openshift/release/blob/master/tools/hack/golang/package.spec

# debuginfo not supported with Go
%global debug_package %{nil}

# modifying the Go binaries breaks the DWARF debugging
%global __os_install_post %{_rpmconfigdir}/brp-compress

%global golang_version 1.24.0
%{!?version: %global version 0.1.2}
%{!?release: %global release 1}

Name: crio-credential-provider
Version: %{version}
Release: %{release}%{?dist}
Summary: CRI-O kubelet image credential provider
License: ASL 2.0

Source0: %{name}.tar.gz
BuildRequires: golang >= %{golang_version}

ExclusiveArch: x86_64 aarch64 ppc64le s390x

%description
This package provides crio-credential-provider. It is a kubelet image
credential provider built for CRI-O to authenticate image pulls against
registry mirrors by using namespaced Kubernetes Secrets. For more information
see: https://kubernetes.io/docs/tasks/administer-cluster/kubelet-credential-provider/

%prep
%autosetup

%build
go build \
    -trimpath \
    -ldflags="-s -w -X 'github.com/cri-o/crio-credential-provider/internal/pkg/version.buildDate=$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)' -X 'github.com/cri-o/crio-credential-provider/pkg/config.RegistriesConfPath=/etc/containers/registries.conf'" \
    -o=crio-credential-provider \
    ./cmd/crio-credential-provider

%install
install -d %{buildroot}%{_libexecdir}/kubelet-image-credential-provider-plugins
install -p -m 755 crio-credential-provider %{buildroot}%{_libexecdir}/kubelet-image-credential-provider-plugins/crio-credential-provider

%files
%license LICENSE
%{_libexecdir}/kubelet-image-credential-provider-plugins/crio-credential-provider

%changelog
* Wed Dec 03 2025 Sascha Grunert <sgrunert@redhat.com> - 0.1.2
- Update to 0.1.2

* Thu Nov 28 2024 Sascha Grunert <sgrunert@redhat.com> - 0.0.1
- Initial package
