package conformance

import (
	"sort"
	"testing"

	ucp "github.com/chaz8081/ucp-go"
	"github.com/chaz8081/ucp-go/common"
	"github.com/chaz8081/ucp-go/shopping"
	"github.com/chaz8081/ucp-go/shopping/types"
	"github.com/chaz8081/ucp-go/transports"
)

// validator is the uniform interface every generated type satisfies.
type validator interface{ Validate() error }

// models maps a schema location to a fresh value of the Go type it
// produces. A location is a schema path for a file-level type, or
// "<path>#<defName>" for a type emitted from a $def.
//
// Go cannot construct a type from a name, so driving the corpus by schema
// location needs an explicit table. It is generated from the emitter's type
// index rather than hand-written, and TestModelsCoverCorpus and
// TestModelsCoverDefs below fail if it ever falls behind the corpus.
//
// Only locations that emit a type appear: the rest — a document root that is
// nothing but a $defs container, a $def that is a namespace grouping other
// schemas — produce no value to construct.
var models = map[string]func() validator{
	"shopping/cart.json":                                              func() validator { return new(shopping.Cart) },
	"shopping/cart_create_request.json":                               func() validator { return new(shopping.CartCreateRequest) },
	"shopping/cart_update_request.json":                               func() validator { return new(shopping.CartUpdateRequest) },
	"shopping/catalog_lookup.json":                                    func() validator { return new(shopping.CatalogLookup) },
	"shopping/catalog_search.json":                                    func() validator { return new(shopping.CatalogSearch) },
	"shopping/checkout.json":                                          func() validator { return new(shopping.Checkout) },
	"shopping/checkout_complete_request.json":                         func() validator { return new(shopping.CheckoutCompleteRequest) },
	"shopping/checkout_create_request.json":                           func() validator { return new(shopping.CheckoutCreateRequest) },
	"shopping/checkout_update_request.json":                           func() validator { return new(shopping.CheckoutUpdateRequest) },
	"shopping/order.json":                                             func() validator { return new(shopping.Order) },
	"shopping/order_create_request.json":                              func() validator { return new(shopping.OrderCreateRequest) },
	"shopping/order_update_request.json":                              func() validator { return new(shopping.OrderUpdateRequest) },
	"shopping/payment.json":                                           func() validator { return new(shopping.Payment) },
	"shopping/payment_complete_request.json":                          func() validator { return new(shopping.PaymentCompleteRequest) },
	"shopping/payment_create_request.json":                            func() validator { return new(shopping.PaymentCreateRequest) },
	"shopping/payment_update_request.json":                            func() validator { return new(shopping.PaymentUpdateRequest) },
	"shopping/types/account_info.json":                                func() validator { return new(types.PaymentAccountInfo) },
	"shopping/types/adjustment.json":                                  func() validator { return new(types.Adjustment) },
	"shopping/types/adjustment_create_request.json":                   func() validator { return new(types.AdjustmentCreateRequest) },
	"shopping/types/adjustment_update_request.json":                   func() validator { return new(types.AdjustmentUpdateRequest) },
	"shopping/types/amount.json":                                      func() validator { return new(types.Amount) },
	"shopping/types/attribution.json":                                 func() validator { return new(types.Attribution) },
	"shopping/types/attribution_complete_request.json":                func() validator { return new(types.AttributionCompleteRequest) },
	"shopping/types/attribution_create_request.json":                  func() validator { return new(types.AttributionCreateRequest) },
	"shopping/types/attribution_update_request.json":                  func() validator { return new(types.AttributionUpdateRequest) },
	"shopping/types/available_payment_instrument.json":                func() validator { return new(types.AvailablePaymentInstrument) },
	"shopping/types/binding.json":                                     func() validator { return new(types.Binding) },
	"shopping/types/business_fulfillment_config.json":                 func() validator { return new(types.BusinessFulfillmentConfig) },
	"shopping/types/buyer.json":                                       func() validator { return new(types.Buyer) },
	"shopping/types/buyer_create_request.json":                        func() validator { return new(types.BuyerCreateRequest) },
	"shopping/types/buyer_update_request.json":                        func() validator { return new(types.BuyerUpdateRequest) },
	"shopping/types/card_credential.json":                             func() validator { return new(types.CardCredential) },
	"shopping/types/card_payment_instrument.json":                     func() validator { return new(types.CardPaymentInstrument) },
	"shopping/types/category.json":                                    func() validator { return new(types.Category) },
	"shopping/types/context.json":                                     func() validator { return new(types.Context) },
	"shopping/types/context_create_request.json":                      func() validator { return new(types.ContextCreateRequest) },
	"shopping/types/context_update_request.json":                      func() validator { return new(types.ContextUpdateRequest) },
	"shopping/types/description.json":                                 func() validator { return new(types.Description) },
	"shopping/types/detail_option_value.json":                         func() validator { return new(types.DetailOptionValue) },
	"shopping/types/error_code.json":                                  func() validator { return new(types.ErrorCode) },
	"shopping/types/error_response.json":                              func() validator { return new(types.ErrorResponse) },
	"shopping/types/expectation.json":                                 func() validator { return new(types.Expectation) },
	"shopping/types/expectation_create_request.json":                  func() validator { return new(types.ExpectationCreateRequest) },
	"shopping/types/expectation_update_request.json":                  func() validator { return new(types.ExpectationUpdateRequest) },
	"shopping/types/fulfillment.json":                                 func() validator { return new(types.Fulfillment) },
	"shopping/types/fulfillment_available_method.json":                func() validator { return new(types.FulfillmentAvailableMethod) },
	"shopping/types/fulfillment_available_method_create_request.json": func() validator { return new(types.FulfillmentAvailableMethodCreateRequest) },
	"shopping/types/fulfillment_available_method_update_request.json": func() validator { return new(types.FulfillmentAvailableMethodUpdateRequest) },
	"shopping/types/fulfillment_create_request.json":                  func() validator { return new(types.FulfillmentCreateRequest) },
	"shopping/types/fulfillment_destination.json":                     func() validator { return new(types.FulfillmentDestination) },
	"shopping/types/fulfillment_destination_create_request.json":      func() validator { return new(types.FulfillmentDestinationCreateRequest) },
	"shopping/types/fulfillment_destination_update_request.json":      func() validator { return new(types.FulfillmentDestinationUpdateRequest) },
	"shopping/types/fulfillment_event.json":                           func() validator { return new(types.FulfillmentEvent) },
	"shopping/types/fulfillment_event_create_request.json":            func() validator { return new(types.FulfillmentEventCreateRequest) },
	"shopping/types/fulfillment_event_update_request.json":            func() validator { return new(types.FulfillmentEventUpdateRequest) },
	"shopping/types/fulfillment_group.json":                           func() validator { return new(types.FulfillmentGroup) },
	"shopping/types/fulfillment_group_create_request.json":            func() validator { return new(types.FulfillmentGroupCreateRequest) },
	"shopping/types/fulfillment_group_update_request.json":            func() validator { return new(types.FulfillmentGroupUpdateRequest) },
	"shopping/types/fulfillment_method.json":                          func() validator { return new(types.FulfillmentMethod) },
	"shopping/types/fulfillment_method_create_request.json":           func() validator { return new(types.FulfillmentMethodCreateRequest) },
	"shopping/types/fulfillment_method_update_request.json":           func() validator { return new(types.FulfillmentMethodUpdateRequest) },
	"shopping/types/fulfillment_option.json":                          func() validator { return new(types.FulfillmentOption) },
	"shopping/types/fulfillment_option_create_request.json":           func() validator { return new(types.FulfillmentOptionCreateRequest) },
	"shopping/types/fulfillment_option_update_request.json":           func() validator { return new(types.FulfillmentOptionUpdateRequest) },
	"shopping/types/fulfillment_update_request.json":                  func() validator { return new(types.FulfillmentUpdateRequest) },
	"shopping/types/info_code.json":                                   func() validator { return new(types.InfoCode) },
	"shopping/types/input_correlation.json":                           func() validator { return new(types.InputCorrelation) },
	"shopping/types/item.json":                                        func() validator { return new(types.Item) },
	"shopping/types/item_create_request.json":                         func() validator { return new(types.ItemCreateRequest) },
	"shopping/types/item_update_request.json":                         func() validator { return new(types.ItemUpdateRequest) },
	"shopping/types/line_item.json":                                   func() validator { return new(types.LineItem) },
	"shopping/types/line_item_create_request.json":                    func() validator { return new(types.LineItemCreateRequest) },
	"shopping/types/line_item_update_request.json":                    func() validator { return new(types.LineItemUpdateRequest) },
	"shopping/types/link.json":                                        func() validator { return new(types.Link) },
	"shopping/types/media.json":                                       func() validator { return new(types.Media) },
	"shopping/types/merchant_fulfillment_config.json":                 func() validator { return new(types.MerchantFulfillmentConfig) },
	"shopping/types/message.json":                                     func() validator { return new(types.Message) },
	"shopping/types/message_create_request.json":                      func() validator { return new(types.MessageCreateRequest) },
	"shopping/types/message_error.json":                               func() validator { return new(types.MessageError) },
	"shopping/types/message_info.json":                                func() validator { return new(types.MessageInfo) },
	"shopping/types/message_update_request.json":                      func() validator { return new(types.MessageUpdateRequest) },
	"shopping/types/error_code_create_request.json":                   func() validator { return new(types.ErrorCodeCreateRequest) },
	"shopping/types/error_code_update_request.json":                   func() validator { return new(types.ErrorCodeUpdateRequest) },
	"shopping/types/info_code_create_request.json":                    func() validator { return new(types.InfoCodeCreateRequest) },
	"shopping/types/info_code_update_request.json":                    func() validator { return new(types.InfoCodeUpdateRequest) },
	"shopping/types/message_error_create_request.json":                func() validator { return new(types.MessageErrorCreateRequest) },
	"shopping/types/message_error_update_request.json":                func() validator { return new(types.MessageErrorUpdateRequest) },
	"shopping/types/message_info_create_request.json":                 func() validator { return new(types.MessageInfoCreateRequest) },
	"shopping/types/message_info_update_request.json":                 func() validator { return new(types.MessageInfoUpdateRequest) },
	"shopping/types/message_warning_create_request.json":              func() validator { return new(types.MessageWarningCreateRequest) },
	"shopping/types/message_warning_update_request.json":              func() validator { return new(types.MessageWarningUpdateRequest) },
	"shopping/types/signed_amount_create_request.json":                func() validator { return new(types.SignedAmountCreateRequest) },
	"shopping/types/signed_amount_update_request.json":                func() validator { return new(types.SignedAmountUpdateRequest) },
	"shopping/types/warning_code_create_request.json":                 func() validator { return new(types.WarningCodeCreateRequest) },
	"shopping/types/warning_code_update_request.json":                 func() validator { return new(types.WarningCodeUpdateRequest) },
	"shopping/types/message_warning.json":                             func() validator { return new(types.MessageWarning) },
	"shopping/types/option_value.json":                                func() validator { return new(types.OptionValue) },
	"shopping/types/order_confirmation.json":                          func() validator { return new(types.OrderConfirmation) },
	"shopping/types/order_line_item.json":                             func() validator { return new(types.OrderLineItem) },
	"shopping/types/order_line_item_create_request.json":              func() validator { return new(types.OrderLineItemCreateRequest) },
	"shopping/types/order_line_item_update_request.json":              func() validator { return new(types.OrderLineItemUpdateRequest) },
	"shopping/types/pagination.json":                                  func() validator { return new(types.Pagination) },
	"shopping/types/payment_credential.json":                          func() validator { return new(types.PaymentCredential) },
	"shopping/types/payment_credential_complete_request.json":         func() validator { return new(types.PaymentCredentialCompleteRequest) },
	"shopping/types/payment_credential_create_request.json":           func() validator { return new(types.PaymentCredentialCreateRequest) },
	"shopping/types/payment_credential_update_request.json":           func() validator { return new(types.PaymentCredentialUpdateRequest) },
	"shopping/types/payment_identity.json":                            func() validator { return new(types.PaymentIdentity) },
	"shopping/types/payment_instrument.json":                          func() validator { return new(types.PaymentInstrument) },
	"shopping/types/payment_instrument_complete_request.json":         func() validator { return new(types.PaymentInstrumentCompleteRequest) },
	"shopping/types/payment_instrument_create_request.json":           func() validator { return new(types.PaymentInstrumentCreateRequest) },
	"shopping/types/payment_instrument_update_request.json":           func() validator { return new(types.PaymentInstrumentUpdateRequest) },
	"shopping/types/platform_fulfillment_config.json":                 func() validator { return new(types.PlatformFulfillmentConfig) },
	"shopping/types/postal_address.json":                              func() validator { return new(types.PostalAddress) },
	"shopping/types/postal_address_complete_request.json":             func() validator { return new(types.PostalAddressCompleteRequest) },
	"shopping/types/postal_address_create_request.json":               func() validator { return new(types.PostalAddressCreateRequest) },
	"shopping/types/postal_address_update_request.json":               func() validator { return new(types.PostalAddressUpdateRequest) },
	"shopping/types/price.json":                                       func() validator { return new(types.Price) },
	"shopping/types/price_filter.json":                                func() validator { return new(types.PriceFilter) },
	"shopping/types/price_range.json":                                 func() validator { return new(types.PriceRange) },
	"shopping/types/product.json":                                     func() validator { return new(types.Product) },
	"shopping/types/product_option.json":                              func() validator { return new(types.ProductOption) },
	"shopping/types/rating.json":                                      func() validator { return new(types.Rating) },
	"shopping/types/retail_location.json":                             func() validator { return new(types.RetailLocation) },
	"shopping/types/retail_location_create_request.json":              func() validator { return new(types.RetailLocationCreateRequest) },
	"shopping/types/retail_location_update_request.json":              func() validator { return new(types.RetailLocationUpdateRequest) },
	"shopping/types/reverse_domain_name.json":                         func() validator { return new(types.ReverseDomainName) },
	"shopping/types/reverse_domain_name_create_request.json":          func() validator { return new(types.ReverseDomainNameCreateRequest) },
	"shopping/types/reverse_domain_name_update_request.json":          func() validator { return new(types.ReverseDomainNameUpdateRequest) },
	"shopping/types/search_filters.json":                              func() validator { return new(types.SearchFilters) },
	"shopping/types/selected_option.json":                             func() validator { return new(types.SelectedOption) },
	"shopping/types/shipping_destination.json":                        func() validator { return new(types.ShippingDestination) },
	"shopping/types/shipping_destination_create_request.json":         func() validator { return new(types.ShippingDestinationCreateRequest) },
	"shopping/types/shipping_destination_update_request.json":         func() validator { return new(types.ShippingDestinationUpdateRequest) },
	"shopping/types/signals.json":                                     func() validator { return new(types.Signals) },
	"shopping/types/signals_complete_request.json":                    func() validator { return new(types.SignalsCompleteRequest) },
	"shopping/types/signals_create_request.json":                      func() validator { return new(types.SignalsCreateRequest) },
	"shopping/types/signals_update_request.json":                      func() validator { return new(types.SignalsUpdateRequest) },
	"shopping/types/signed_amount.json":                               func() validator { return new(types.SignedAmount) },
	"shopping/types/token_credential.json":                            func() validator { return new(types.TokenCredential) },
	"shopping/types/total.json":                                       func() validator { return new(types.Total) },
	"shopping/types/total_create_request.json":                        func() validator { return new(types.TotalCreateRequest) },
	"shopping/types/total_update_request.json":                        func() validator { return new(types.TotalUpdateRequest) },
	"shopping/types/totals.json":                                      func() validator { return new(types.Totals) },
	"shopping/types/totals_create_request.json":                       func() validator { return new(types.TotalsCreateRequest) },
	"shopping/types/totals_update_request.json":                       func() validator { return new(types.TotalsUpdateRequest) },
	"shopping/types/variant.json":                                     func() validator { return new(types.Variant) },
	"shopping/types/warning_code.json":                                func() validator { return new(types.WarningCode) },
	"transports/embedded_config.json":                                 func() validator { return new(transports.EmbeddedTransportConfig) },
	"ucp.json":                                                        func() validator { return new(ucp.UCPMetadata) },
	"ucp_create_request.json":                                         func() validator { return new(ucp.UCPMetadataCreateRequest) },
	"ucp_update_request.json":                                         func() validator { return new(ucp.UCPMetadataUpdateRequest) },

	// $defs-derived types, keyed "<rel>#<defName>". Iterating schema files
	// alone reaches only the file-level type, which left every $def type
	// unexercised — including the whole capability model, all of fulfillment
	// and discount, and ap2_mandate: those eight files declare no file-level
	// type at all, so the harness skipped them whole.
	//
	// Every $def that emits a type is registered, not only those in the eight
	// files lacking a root type. A $def in a file that also has a root type is
	// just as untested, because the root type is a different Go struct that
	// merely happens to share the document.
	"capability.json#base":                                                                func() validator { return new(ucp.CapabilityBase) },
	"capability.json#business_schema":                                                     func() validator { return new(ucp.CapabilityBusinessSchema) },
	"capability.json#platform_schema":                                                     func() validator { return new(ucp.CapabilityPlatformSchema) },
	"capability.json#response_schema":                                                     func() validator { return new(ucp.CapabilityResponseSchema) },
	"common/identity_linking.json#scope_policy":                                           func() validator { return new(common.IdentityLinkingScopePolicy) },
	"common/identity_linking.json#scope_token":                                            func() validator { return new(common.IdentityLinkingScopeToken) },
	"payment_handler.json#base":                                                           func() validator { return new(ucp.PaymentHandlerBase) },
	"payment_handler.json#business_schema":                                                func() validator { return new(ucp.PaymentHandlerBusinessSchema) },
	"payment_handler.json#platform_schema":                                                func() validator { return new(ucp.PaymentHandlerPlatformSchema) },
	"payment_handler.json#response_schema":                                                func() validator { return new(ucp.PaymentHandlerResponseSchema) },
	"service.json#base":                                                                   func() validator { return new(ucp.ServiceBase) },
	"service.json#business_schema":                                                        func() validator { return new(ucp.ServiceBusinessSchema) },
	"service.json#platform_schema":                                                        func() validator { return new(ucp.ServicePlatformSchema) },
	"service.json#response_schema":                                                        func() validator { return new(ucp.ServiceResponseSchema) },
	"shopping/ap2_mandate.json#ap2_with_checkout_mandate":                                 func() validator { return new(shopping.AP2MandateAP2WithCheckoutMandate) },
	"shopping/ap2_mandate.json#ap2_with_merchant_authorization":                           func() validator { return new(shopping.AP2MandateAP2WithMerchantAuthorization) },
	"shopping/ap2_mandate.json#checkout":                                                  func() validator { return new(shopping.AP2MandateCheckout) },
	"shopping/ap2_mandate.json#checkout_mandate":                                          func() validator { return new(shopping.AP2MandateCheckoutMandate) },
	"shopping/ap2_mandate.json#error_code":                                                func() validator { return new(shopping.AP2MandateErrorCode) },
	"shopping/ap2_mandate.json#merchant_authorization":                                    func() validator { return new(shopping.AP2MandateMerchantAuthorization) },
	"shopping/buyer_consent.json#buyer":                                                   func() validator { return new(shopping.BuyerConsentBuyer) },
	"shopping/buyer_consent.json#checkout":                                                func() validator { return new(shopping.BuyerConsentCheckout) },
	"shopping/buyer_consent.json#consent":                                                 func() validator { return new(shopping.BuyerConsentConsent) },
	"shopping/cart.json#checkout":                                                         func() validator { return new(shopping.CartCheckout) },
	"shopping/cart_create_request.json#checkout":                                          func() validator { return new(shopping.CartCreateRequestCheckout) },
	"shopping/cart_update_request.json#checkout":                                          func() validator { return new(shopping.CartUpdateRequestCheckout) },
	"shopping/catalog_lookup.json#detail_product":                                         func() validator { return new(shopping.CatalogLookupDetailProduct) },
	"shopping/catalog_lookup.json#get_product_request":                                    func() validator { return new(shopping.CatalogLookupGetProductRequest) },
	"shopping/catalog_lookup.json#get_product_response":                                   func() validator { return new(shopping.CatalogLookupGetProductResponse) },
	"shopping/catalog_lookup.json#lookup_request":                                         func() validator { return new(shopping.CatalogLookupLookupRequest) },
	"shopping/catalog_lookup.json#lookup_response":                                        func() validator { return new(shopping.CatalogLookupLookupResponse) },
	"shopping/catalog_lookup.json#lookup_variant":                                         func() validator { return new(shopping.CatalogLookupLookupVariant) },
	"shopping/catalog_search.json#search_request":                                         func() validator { return new(shopping.CatalogSearchSearchRequest) },
	"shopping/catalog_search.json#search_response":                                        func() validator { return new(shopping.CatalogSearchSearchResponse) },
	"shopping/discount.json#allocation":                                                   func() validator { return new(shopping.DiscountAllocation) },
	"shopping/discount.json#applied_discount":                                             func() validator { return new(shopping.DiscountAppliedDiscount) },
	"shopping/discount.json#cart":                                                         func() validator { return new(shopping.DiscountCart) },
	"shopping/discount.json#checkout":                                                     func() validator { return new(shopping.DiscountCheckout) },
	"shopping/discount.json#discounts_object":                                             func() validator { return new(shopping.DiscountDiscountsObject) },
	"shopping/fulfillment.json#checkout":                                                  func() validator { return new(shopping.FulfillmentCheckout) },
	"shopping/fulfillment.json#fulfillment":                                               func() validator { return new(shopping.FulfillmentFulfillment) },
	"shopping/fulfillment.json#fulfillment_available_method":                              func() validator { return new(shopping.FulfillmentFulfillmentAvailableMethod) },
	"shopping/fulfillment.json#fulfillment_group":                                         func() validator { return new(shopping.FulfillmentFulfillmentGroup) },
	"shopping/fulfillment.json#fulfillment_method":                                        func() validator { return new(shopping.FulfillmentFulfillmentMethod) },
	"shopping/fulfillment.json#fulfillment_option":                                        func() validator { return new(shopping.FulfillmentFulfillmentOption) },
	"shopping/order.json#platform_schema":                                                 func() validator { return new(shopping.OrderPlatformSchema) },
	"shopping/order_create_request.json#platform_schema":                                  func() validator { return new(shopping.OrderCreateRequestPlatformSchema) },
	"shopping/order_update_request.json#platform_schema":                                  func() validator { return new(shopping.OrderUpdateRequestPlatformSchema) },
	"shopping/types/card_payment_instrument.json#available_card_payment_instrument":       func() validator { return new(types.CardPaymentInstrumentAvailableCardPaymentInstrument) },
	"shopping/types/pagination.json#request":                                              func() validator { return new(types.PaginationRequest) },
	"shopping/types/pagination.json#response":                                             func() validator { return new(types.PaginationResponse) },
	"shopping/types/payment_instrument.json#selected_payment_instrument":                  func() validator { return new(types.PaymentInstrumentSelectedPaymentInstrument) },
	"shopping/types/payment_instrument_complete_request.json#selected_payment_instrument": func() validator { return new(types.PaymentInstrumentCompleteRequestSelectedPaymentInstrument) },
	"shopping/types/payment_instrument_create_request.json#selected_payment_instrument":   func() validator { return new(types.PaymentInstrumentCreateRequestSelectedPaymentInstrument) },
	"shopping/types/payment_instrument_update_request.json#selected_payment_instrument":   func() validator { return new(types.PaymentInstrumentUpdateRequestSelectedPaymentInstrument) },
	"ucp.json#base":                                    func() validator { return new(ucp.UCPBase) },
	"ucp.json#business_schema":                         func() validator { return new(ucp.UCPBusinessSchema) },
	"ucp.json#entity":                                  func() validator { return new(ucp.UCPEntity) },
	"ucp.json#error":                                   func() validator { return new(ucp.UCPError) },
	"ucp.json#platform_schema":                         func() validator { return new(ucp.UCPPlatformSchema) },
	"ucp.json#requires":                                func() validator { return new(ucp.UCPRequires) },
	"ucp.json#response_cart_schema":                    func() validator { return new(ucp.UCPResponseCartSchema) },
	"ucp.json#response_catalog_schema":                 func() validator { return new(ucp.UCPResponseCatalogSchema) },
	"ucp.json#response_checkout_schema":                func() validator { return new(ucp.UCPResponseCheckoutSchema) },
	"ucp.json#response_order_schema":                   func() validator { return new(ucp.UCPResponseOrderSchema) },
	"ucp.json#success":                                 func() validator { return new(ucp.UCPSuccess) },
	"ucp.json#version":                                 func() validator { return new(ucp.UCPVersion) },
	"ucp.json#version_constraint":                      func() validator { return new(ucp.UCPVersionConstraint) },
	"ucp_create_request.json#base":                     func() validator { return new(ucp.UCPCreateRequestBase) },
	"ucp_create_request.json#business_schema":          func() validator { return new(ucp.UCPCreateRequestBusinessSchema) },
	"ucp_create_request.json#entity":                   func() validator { return new(ucp.UCPCreateRequestEntity) },
	"ucp_create_request.json#error":                    func() validator { return new(ucp.UCPCreateRequestError) },
	"ucp_create_request.json#platform_schema":          func() validator { return new(ucp.UCPCreateRequestPlatformSchema) },
	"ucp_create_request.json#requires":                 func() validator { return new(ucp.UCPCreateRequestRequires) },
	"ucp_create_request.json#response_cart_schema":     func() validator { return new(ucp.UCPCreateRequestResponseCartSchema) },
	"ucp_create_request.json#response_catalog_schema":  func() validator { return new(ucp.UCPCreateRequestResponseCatalogSchema) },
	"ucp_create_request.json#response_checkout_schema": func() validator { return new(ucp.UCPCreateRequestResponseCheckoutSchema) },
	"ucp_create_request.json#response_order_schema":    func() validator { return new(ucp.UCPCreateRequestResponseOrderSchema) },
	"ucp_create_request.json#success":                  func() validator { return new(ucp.UCPCreateRequestSuccess) },
	"ucp_create_request.json#version":                  func() validator { return new(ucp.UCPCreateRequestVersion) },
	"ucp_create_request.json#version_constraint":       func() validator { return new(ucp.UCPCreateRequestVersionConstraint) },
	"ucp_update_request.json#base":                     func() validator { return new(ucp.UCPUpdateRequestBase) },
	"ucp_update_request.json#business_schema":          func() validator { return new(ucp.UCPUpdateRequestBusinessSchema) },
	"ucp_update_request.json#entity":                   func() validator { return new(ucp.UCPUpdateRequestEntity) },
	"ucp_update_request.json#error":                    func() validator { return new(ucp.UCPUpdateRequestError) },
	"ucp_update_request.json#platform_schema":          func() validator { return new(ucp.UCPUpdateRequestPlatformSchema) },
	"ucp_update_request.json#requires":                 func() validator { return new(ucp.UCPUpdateRequestRequires) },
	"ucp_update_request.json#response_cart_schema":     func() validator { return new(ucp.UCPUpdateRequestResponseCartSchema) },
	"ucp_update_request.json#response_catalog_schema":  func() validator { return new(ucp.UCPUpdateRequestResponseCatalogSchema) },
	"ucp_update_request.json#response_checkout_schema": func() validator { return new(ucp.UCPUpdateRequestResponseCheckoutSchema) },
	"ucp_update_request.json#response_order_schema":    func() validator { return new(ucp.UCPUpdateRequestResponseOrderSchema) },
	"ucp_update_request.json#success":                  func() validator { return new(ucp.UCPUpdateRequestSuccess) },
	"ucp_update_request.json#version":                  func() validator { return new(ucp.UCPUpdateRequestVersion) },
	"ucp_update_request.json#version_constraint":       func() validator { return new(ucp.UCPUpdateRequestVersionConstraint) },
}

// TestModelsCoverCorpus keeps the table honest. Without it a schema added
// upstream would simply be skipped by the differential harness, quietly
// shrinking coverage.
func TestModelsCoverCorpus(t *testing.T) {
	set := loadGoldens(t)
	idx := buildIndex(t, set)
	var missing []string
	for _, rel := range sortedKeys(set) {
		if _, ok := idx.Lookup(rel, ""); !ok {
			continue // no file-level type, nothing to construct
		}
		if _, ok := models[rel]; !ok {
			missing = append(missing, rel)
		}
	}
	if len(missing) > 0 {
		t.Errorf("models is missing %d schemas that emit a file-level type: %v", len(missing), missing)
	}
}

// TestModelsCoverDefs is the same guarantee for $defs-derived types, and it
// exists because iterating schema files was never the same thing as
// iterating emitted types. Eight files put all their content in $defs and so
// have no file-level type; the harness looked them up by file, missed, and
// skipped them whole, taking 34 emitted types — the entire capability model
// among them — out of the comparison without ever naming them as a gap.
//
// A $def with no emitted type is not a failure here. The two grouping
// objects in the corpus, identity_linking and dev_ucp_shopping_fulfillment,
// are namespaces whose values are themselves schemas; the emitter produces
// nothing for them by design, so there is nothing to register.
func TestModelsCoverDefs(t *testing.T) {
	set := loadGoldens(t)
	idx := buildIndex(t, set)
	var missing []string
	for _, rel := range sortedKeys(set) {
		defs, _ := set[rel]["$defs"].(map[string]any)
		names := make([]string, 0, len(defs))
		for name := range defs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, ok := idx.Lookup(rel, name); !ok {
				continue // a grouping object, no type emitted
			}
			if _, ok := models[rel+"#"+name]; !ok {
				missing = append(missing, rel+"#"+name)
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("models is missing %d $defs types: %v", len(missing), missing)
	}
}
