package tests

// Expected outputs for the two exhaustive cases (1 and 4). Key values were
// checked by hand against docs/test_specs.md; run summary timing columns are
// excluded.

const expectedSingleTripTrips = "request_id,status,requested_at,num_requested_seats,first_assigned_at,assigned_at,supply_id,reassign_count,ridepool_type,eta_at_assignment_seconds,driver_arrived_at,pickup_at,dropoff_at,wait_seconds,trip_distance_meters,trip_duration_seconds,direct_distance_meters,direct_duration_seconds,detour_ratio,pickup_distance_meters,max_co_riders,canceled_at,cancel_reason\n" +
	"kezar_stadium_to_coit_tower,completed,1767225660,1,1767225660,1767225660,car_at_kezar_stadium,0,p0d0,0,1767225660,1767225660,1767226254,0,5940,594,5940,594,1.000000,0,0,,\n"

const expectedSingleTripSupplyEventLog = "supply_id,at,action,lat,lng,state_after,request_id,plan_id,seats_in_use,odometer_meters,driven_polyline,planned_polyline,plan\n" +
	"car_at_kezar_stadium,1767225600,online,37.767263,-122.456743,idle,,,0,0,,,\n" +
	"car_at_kezar_stadium,1767225660,plan_updated,37.767263,-122.456743,en_route,,car_at_kezar_stadium_plan_1,0,0,,kloeFriljV??qzEg}H,\"[{\"\"request_id\"\":\"\"kezar_stadium_to_coit_tower\"\",\"\"type\"\":\"\"pickup\"\",\"\"lat\"\":37.767263,\"\"lng\"\":-122.456743,\"\"eta_at\"\":1767225660},{\"\"request_id\"\":\"\"kezar_stadium_to_coit_tower\"\",\"\"type\"\":\"\"dropoff\"\",\"\"lat\"\":37.802395,\"\"lng\"\":-122.405822,\"\"eta_at\"\":1767226254}]\"\n" +
	"car_at_kezar_stadium,1767225660,arrived_pickup,37.767263,-122.456743,en_route,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_1,0,0,,,\n" +
	"car_at_kezar_stadium,1767225660,pickup,37.767263,-122.456743,on_trip,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_1,1,0,,,\n" +
	"car_at_kezar_stadium,1767226254,arrived_dropoff,37.802395,-122.405822,on_trip,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_1,1,5940,kloeFriljVqzEg}H,,\n" +
	"car_at_kezar_stadium,1767226254,dropoff,37.802395,-122.405822,idle,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_1,0,5940,,,\n" +
	"car_at_kezar_stadium,1767226254,idle,37.802395,-122.405822,idle,,,0,5940,,,\n"

const expectedSingleTripSupplySummary = "supply_id,online_at,offline_at,idle_seconds,en_route_seconds,on_trip_seconds,idle_meters,en_route_meters,on_trip_meters,requests_served,utilization,deadhead_ratio,full_polyline\n" +
	"car_at_kezar_stadium,1767225600,,60,0,594,0,0,5940,1,0.908257,0.000000,kloeFriljVqzEg}H\n"

const expectedSingleTripRunSummary = "requests_completed,requests_canceled,requests_canceled_max_request_to_assignment,requests_canceled_max_request_to_pickup,requests_unserved,median_wait_seconds,p90_wait_seconds,mean_detour_ratio,fleet_utilization,matcher_calls_request,matcher_calls_supply_online,matcher_calls_supply_offline,matcher_calls_batch_tick\n" +
	"1,0,0,0,0,0,0,1.000000,0.908257,1,0,0,0\n"

const expectedSingleTripRunMetadata = "simulator_version,requests_sha256,supply_sha256,settings_sha256,settings,simulation_start_at,simulation_end_at\n" +
	"0.2.0,b1aa6335240f473bb130cf8fc31a88f02853995c08802ec05a6b3b3e55812f99,bc6a8e3a0b755bb74f14e1775eb04b1fedd856fc3f63c50734a34b475fd6e574,214b4f5114de7e1b086ccc233258c6aae90a99f2aa44e3b4f4ddeb3f205fcddf,\"{\"\"max_request_to_assignment_seconds\"\":null,\"\"max_request_to_pickup_seconds\"\":null,\"\"pooling_enabled\"\":false,\"\"reassignment_enabled\"\":true,\"\"batch_matching_interval_seconds\"\":null,\"\"pickup_dwell_seconds\"\":0,\"\"dropoff_dwell_seconds\"\":0,\"\"default_num_requested_seats\"\":1,\"\"default_num_available_seats\"\":4,\"\"average_speed_meters_per_second\"\":10,\"\"matching_hook\"\":\"\"reference_test_matcher\"\",\"\"routing_hook\"\":\"\"straight_line\"\"}\",1767225660,1767226254\n"

const expectedSimplePoolTrips = "request_id,status,requested_at,num_requested_seats,first_assigned_at,assigned_at,supply_id,reassign_count,ridepool_type,eta_at_assignment_seconds,driver_arrived_at,pickup_at,dropoff_at,wait_seconds,trip_distance_meters,trip_duration_seconds,direct_distance_meters,direct_duration_seconds,detour_ratio,pickup_distance_meters,max_co_riders,canceled_at,cancel_reason\n" +
	"kezar_stadium_to_coit_tower,completed,1767225660,1,1767225660,1767225660,car_at_kezar_stadium,0,p0p1d1d0,0,1767225660,1767225660,1767226315,0,6554,655,5940,594,1.103367,0,1,,\n" +
	"painted_ladies_to_union_square,completed,1767225720,1,1767225720,1767225720,car_at_kezar_stadium,0,p0d0,176,1767225896,1767225896,1767226154,176,2583,258,2583,258,1.000000,1763,1,,\n"

const expectedSimplePoolSupplyEventLog = "supply_id,at,action,lat,lng,state_after,request_id,plan_id,seats_in_use,odometer_meters,driven_polyline,planned_polyline,plan\n" +
	"car_at_kezar_stadium,1767225600,online,37.767263,-122.456743,idle,,,0,0,,,\n" +
	"car_at_kezar_stadium,1767225660,plan_updated,37.767263,-122.456743,en_route,,car_at_kezar_stadium_plan_1,0,0,,kloeFriljV??qzEg}H,\"[{\"\"request_id\"\":\"\"kezar_stadium_to_coit_tower\"\",\"\"type\"\":\"\"pickup\"\",\"\"lat\"\":37.767263,\"\"lng\"\":-122.456743,\"\"eta_at\"\":1767225660},{\"\"request_id\"\":\"\"kezar_stadium_to_coit_tower\"\",\"\"type\"\":\"\"dropoff\"\",\"\"lat\"\":37.802395,\"\"lng\"\":-122.405822,\"\"eta_at\"\":1767226254}]\"\n" +
	"car_at_kezar_stadium,1767225660,arrived_pickup,37.767263,-122.456743,en_route,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_1,0,0,,,\n" +
	"car_at_kezar_stadium,1767225660,pickup,37.767263,-122.456743,on_trip,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_1,1,0,,,\n" +
	"car_at_kezar_stadium,1767225720,plan_updated,37.770812,-122.451599,on_trip,,car_at_kezar_stadium_plan_2,1,600,kloeFriljVeUc_@,qbpeFnikjVe`@cuBeiA{|C_yAcI,\"[{\"\"request_id\"\":\"\"painted_ladies_to_union_square\"\",\"\"type\"\":\"\"pickup\"\",\"\"lat\"\":37.77612,\"\"lng\"\":-122.4327,\"\"eta_at\"\":1767225896},{\"\"request_id\"\":\"\"painted_ladies_to_union_square\"\",\"\"type\"\":\"\"dropoff\"\",\"\"lat\"\":37.787994,\"\"lng\"\":-122.407437,\"\"eta_at\"\":1767226154},{\"\"request_id\"\":\"\"kezar_stadium_to_coit_tower\"\",\"\"type\"\":\"\"dropoff\"\",\"\"lat\"\":37.802395,\"\"lng\"\":-122.405822,\"\"eta_at\"\":1767226315}]\"\n" +
	"car_at_kezar_stadium,1767225896,arrived_pickup,37.776120,-122.432700,on_trip,painted_ladies_to_union_square,car_at_kezar_stadium_plan_2,1,2363,qbpeFnikjVe`@cuB,,\n" +
	"car_at_kezar_stadium,1767225896,pickup,37.776120,-122.432700,on_trip,painted_ladies_to_union_square,car_at_kezar_stadium_plan_2,2,2363,,,\n" +
	"car_at_kezar_stadium,1767226154,arrived_dropoff,37.787994,-122.407437,on_trip,painted_ladies_to_union_square,car_at_kezar_stadium_plan_2,2,4946,wcqeFjsgjVeiA{|C,,\n" +
	"car_at_kezar_stadium,1767226154,dropoff,37.787994,-122.407437,on_trip,painted_ladies_to_union_square,car_at_kezar_stadium_plan_2,1,4946,,,\n" +
	"car_at_kezar_stadium,1767226315,arrived_dropoff,37.802395,-122.405822,on_trip,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_2,1,6554,}mseFnubjV_yAcI,,\n" +
	"car_at_kezar_stadium,1767226315,dropoff,37.802395,-122.405822,idle,kezar_stadium_to_coit_tower,car_at_kezar_stadium_plan_2,0,6554,,,\n" +
	"car_at_kezar_stadium,1767226315,idle,37.802395,-122.405822,idle,,,0,6554,,,\n"

const expectedSimplePoolSupplySummary = "supply_id,online_at,offline_at,idle_seconds,en_route_seconds,on_trip_seconds,idle_meters,en_route_meters,on_trip_meters,requests_served,utilization,deadhead_ratio,full_polyline\n" +
	"car_at_kezar_stadium,1767225600,,60,0,655,0,0,6554,2,0.916084,0.000000,kloeFriljVeUc_@e`@cuBeiA{|C_yAcI\n"

const expectedSimplePoolRunSummary = "requests_completed,requests_canceled,requests_canceled_max_request_to_assignment,requests_canceled_max_request_to_pickup,requests_unserved,median_wait_seconds,p90_wait_seconds,mean_detour_ratio,fleet_utilization,matcher_calls_request,matcher_calls_supply_online,matcher_calls_supply_offline,matcher_calls_batch_tick\n" +
	"2,0,0,0,0,0,176,1.051684,0.916084,2,0,0,0\n"

const expectedSimplePoolRunMetadata = "simulator_version,requests_sha256,supply_sha256,settings_sha256,settings,simulation_start_at,simulation_end_at\n" +
	"0.2.0,431933658e63f0fa7e99b63d1aa0da70e5fae714897a6824549b6c992f2207f9,bc6a8e3a0b755bb74f14e1775eb04b1fedd856fc3f63c50734a34b475fd6e574,167449dc0b5f5a56e72e548c84ad3e88a687cdbd635b622e0fc2f4da27109eb8,\"{\"\"max_request_to_assignment_seconds\"\":null,\"\"max_request_to_pickup_seconds\"\":null,\"\"pooling_enabled\"\":true,\"\"reassignment_enabled\"\":true,\"\"batch_matching_interval_seconds\"\":null,\"\"pickup_dwell_seconds\"\":0,\"\"dropoff_dwell_seconds\"\":0,\"\"default_num_requested_seats\"\":1,\"\"default_num_available_seats\"\":4,\"\"average_speed_meters_per_second\"\":10,\"\"matching_hook\"\":\"\"reference_test_matcher\"\",\"\"routing_hook\"\":\"\"straight_line\"\"}\",1767225660,1767226315\n"
