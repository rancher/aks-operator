package mock_services

// Run go generate to regenerate this mock.
//
//go:generate ../../../../bin/mockgen -destination agentpools_mock.go -package mock_services -source ../agentpools.go AgentPoolsClientInterface
//go:generate ../../../../bin/mockgen -destination groups_mock.go -package mock_services -source ../groups.go ResourceGroupsClientInterface
//go:generate ../../../../bin/mockgen -destination managedclusters_mock.go -package mock_services -source ../managedclusters.go ManagedClustersClientInterface
//go:generate ../../../../bin/mockgen -destination workplaces_mock.go -package mock_services -source ../workplaces.go WorkplacesClientInterface
