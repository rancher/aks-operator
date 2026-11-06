package mock_services

// Run go generate to regenerate this mock.
//
//go:generate go tool -modfile ../../../../gotools/mockgen/go.mod mockgen -destination agentpools_mock.go -package mock_services -source ../agentpools.go AgentPoolsClientInterface
//go:generate go tool -modfile ../../../../gotools/mockgen/go.mod mockgen -destination groups_mock.go -package mock_services -source ../groups.go ResourceGroupsClientInterface
//go:generate go tool -modfile ../../../../gotools/mockgen/go.mod mockgen -destination managedclusters_mock.go -package mock_services -source ../managedclusters.go ManagedClustersClientInterface
//go:generate go tool -modfile ../../../../gotools/mockgen/go.mod mockgen -destination workplaces_mock.go -package mock_services -source ../workplaces.go WorkplacesClientInterface
